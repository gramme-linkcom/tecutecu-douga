package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/jackc/pgx/v4/stdlib" // PostgreSQLドライバ
	"github.com/jmoiron/sqlx"
)

// --- グローバル変数 ---
var db *sqlx.DB
var rdb *redis.Client
var ctx = context.Background()

const redisJobQueue = "encoding_jobs"

// --- 構造体定義  ---
type Video struct {
	UUID          string         `db:"uuid"`
	DisplayID     string         `db:"display_id"`
	Title         string         `db:"title"`
	Description   sql.NullString `db:"description"`
	Status        string         `db:"status"`
	ThumbnailPath sql.NullString `db:"thumbnail_path"`
	VideoFilePath sql.NullString `db:"video_file_path"`
	CreatedAt     time.Time      `db:"created_at"`
}

type EncodingJob struct {
	UUID     string `json:"uuid"`
	TempPath string `json:"temp_path"`
}

// --- 初期化処理 ---
func init() {
	// DBに接続
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	var err error
	for i := 0; i < 5; i++ {
		db, err = sqlx.Connect("pgx", dsn)
		if err == nil {
			log.Println("Worker: DBへの接続に成功しました。")
			break
		}
		log.Printf("Worker: DBへの接続に失敗しました。リトライします... (%d/5)", i+1)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		log.Fatalf("DBへの接続に失敗しました: %v", err)
	}

	// Redisに接続
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "db:6379"
	}
	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatalf("Redisへの接続に失敗しました: %v", err)
	}
	log.Println("Worker: Redisへの接続に成功しました。")
}

// --- メイン関数 ---
func main() {
	log.Println("エンコードワーカーを開始します。ジョブを待機中...")

	for {
		// Redisのリストからジョブをブロッキングで待機＆取得
		result, err := rdb.BRPop(ctx, 0, redisJobQueue).Result()
		if err != nil {
			log.Printf("Redisからのジョブ取得に失敗: %v", err)
			time.Sleep(5 * time.Second) // 接続エラーの場合少し待ってリトライ
			continue
		}

		var job EncodingJob
		if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
			log.Printf("ジョブデータのパースに失敗: %v", err)
			continue
		}

		log.Printf("新しいジョブを受信: UUID=%s", job.UUID)
		processJob(job)
	}
}

// --- ジョブ処理関数 ---
func processJob(job EncodingJob) {
	// 処理完了後に一時ファイルを確実に削除するための defer
	defer os.Remove(job.TempPath)

	// 一時ファイルが存在するか確認
	if _, err := os.Stat(job.TempPath); os.IsNotExist(err) {
		log.Printf("エラー: 一時ファイルが見つかりません: %s", job.TempPath)
		db.Exec("UPDATE videos SET status = 'failed' WHERE uuid = $1", job.UUID)
		return
	}

	streamDir := filepath.Join("static", "streams", job.UUID)
	os.MkdirAll(streamDir, os.ModePerm)

	// サムネイル生成
	thumbnailName := job.UUID + ".jpg"
	thumbnailPath := filepath.Join("static", "thumbnails", thumbnailName)
	cmdThumb := exec.Command("ffmpeg", "-i", job.TempPath, "-ss", "00:00:02", "-vframes", "1", "-f", "image2", "-y", thumbnailPath)
	if err := cmdThumb.Run(); err != nil {
		log.Printf("サムネイル生成に失敗: %v (UUID: %s)", err, job.UUID)
		// 失敗してもHLS変換は続行
	}

	// FFmpegでHLSに変換
	args := []string{
			"-hwaccel", "cuda", // ハードウェアアクセラレーションを明示的に指定
			"-i", job.TempPath,
			"-hide_banner", "-y",

			// --- 1080p ---
			"-vf", "hwupload_cuda,scale_cuda=-2:1080", "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "4000k", "-maxrate", "5000k", "-bufsize", "10000k", "-c:a", "aac", "-b:a", "192k", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(streamDir, "1080p_%04d.ts"), filepath.Join(streamDir, "1080p.m3u8"),
			// --- 720p ---
			"-vf", "hwupload_cuda,scale_cuda=-2:720", "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "2800k", "-maxrate", "3400k", "-bufsize", "5000k", "-c:a", "aac", "-b:a", "128k", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(streamDir, "720p_%04d.ts"), filepath.Join(streamDir, "720p.m3u8"),

			// --- 480p ---
			"-vf", "hwupload_cuda,scale_cuda=-2:480", "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "1400k", "-maxrate", "1700k", "-bufsize", "2500k", "-c:a", "aac", "-b:a", "96k", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(streamDir, "480p_%04d.ts"), filepath.Join(streamDir, "480p.m3u8"),

			// --- 360p ---
			"-vf", "hwupload_cuda,scale_cuda=-2:360", "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "800k", "-maxrate", "1000k", "-bufsize", "1500k", "-c:a", "aac", "-b:a", "96k", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(streamDir, "360p_%04d.ts"), filepath.Join(streamDir, "360p.m3u8"),

			// --- 144p ---
			"-vf", "hwupload_cuda,scale_cuda=-2:144", "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-b:v", "400k", "-maxrate", "500k", "-bufsize", "800k", "-c:a", "aac", "-b:a", "64k", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(streamDir, "144p_%04d.ts"), filepath.Join(streamDir, "144p.m3u8"),
	}
	cmdHLS := exec.Command("ffmpeg", args...)
	output, err := cmdHLS.CombinedOutput()
	if err != nil {
		log.Printf("HLS変換に失敗: UUID=%s, error=%v, output=%s", job.UUID, err, string(output))
		db.Exec("UPDATE videos SET status = 'failed' WHERE uuid = $1", job.UUID)
		return
	}

	masterPlaylistPath := filepath.Join(streamDir, "master.m3u8")
	masterPlaylistContent := `#EXTM3U
	#EXT-X-VERSION:3
	#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,NAME="1080p"
	1080p.m3u8
	#EXT-X-STREAM-INF:BANDWIDTH=3400000,RESOLUTION=1280x720,NAME="720p"
	720p.m3u8
	#EXT-X-STREAM-INF:BANDWIDTH=1700000,RESOLUTION=854x480,NAME="480p"
	480p.m3u8
	#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=640x360,NAME="360p"
	360p.m3u8
	#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=256x144,NAME="144p"
	144p.m3u8
	`
	err = os.WriteFile(masterPlaylistPath, []byte(masterPlaylistContent), 0644)
	if err != nil {
		log.Printf("マスタープレイリスト作成に失敗: %v (UUID: %s)", err, job.UUID)
		db.Exec("UPDATE videos SET status = 'failed' WHERE uuid = $1", job.UUID)
		return
	}

	log.Printf("HLS変換が完了: UUID=%s", job.UUID)

	masterPlaylistURL := "/static/streams/" + job.UUID + "/master.m3u8"
	thumbnailURL := "/static/thumbnails/" + thumbnailName
	query := `UPDATE videos SET status = 'active', video_file_path = $1, thumbnail_path = $2 WHERE uuid = $3`
	_, err = db.Exec(query, masterPlaylistURL, thumbnailURL, job.UUID)
	if err != nil {
		log.Printf("DBの更新に失敗: %v (UUID: %s)", err, job.UUID)
		return
	}
	log.Printf("DBを更新しました: UUID=%s", job.UUID)
}
