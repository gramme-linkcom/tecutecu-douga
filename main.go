package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v4/stdlib" // PostgreSQLドライバ
	"github.com/jmoiron/sqlx"
)

// --- グローバル変数 ---
var db *sqlx.DB
var rdb *redis.Client
var templates *template.Template
var ctx = context.Background()

const redisJobQueue = "encoding_jobs" // Redisのジョブキュー名

// --- 構造体定義 ---

// Video構造体 (DBテーブルに対応)
type Video struct {
	UUID          string         `db:"uuid"`
	DisplayID     string         `db:"display_id"`
	Title         string         `db:"title"`
	Description   sql.NullString `db:"description"` // NULLを許容する文字列
	Status        string         `db:"status"`
	ThumbnailPath sql.NullString `db:"thumbnail_path"`
	VideoFilePath sql.NullString `db:"video_file_path"`
	CreatedAt     time.Time      `db:"created_at"`
}

// エンコードジョブの構造体
type EncodingJob struct {
	UUID     string `json:"uuid"`
	TempPath string `json:"temp_path"`
}

// uploadPageData はアップロードページに渡すためのデータ構造体です。
type uploadPageData struct {
	Status  string
	Message string
}

// --- 初期化処理 ---

func init() {
	// HTMLテンプレートをパース
	templates = template.Must(template.ParseFiles(
		"templates/index.html",
		"templates/video.html",
		"templates/upload.html",
	))

	// DBに接続
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	var err error
	db, err = sqlx.Connect("pgx", dsn)
	if err != nil {
		log.Fatalf("DBへの接続に失敗しました: %v", err)
	}
	log.Println("DBへの接続に成功しました。")

	// テーブル作成
	schema := `
    CREATE TABLE IF NOT EXISTS videos (
        uuid TEXT PRIMARY KEY,
        display_id TEXT UNIQUE NOT NULL,
        title TEXT NOT NULL,
        description TEXT,
        status TEXT NOT NULL,
        thumbnail_path TEXT,
        video_file_path TEXT,
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
    );`
	db.MustExec(schema)
	log.Println("テーブルの準備が完了しました。")

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
	log.Println("Redisへの接続に成功しました。")

	// Redisカウンターの初期化
	counterExists, err := rdb.Exists(ctx, "video_counter").Result()
	if err != nil {
		log.Fatalf("Redisカウンターの確認に失敗: %v", err)
	}
	if counterExists == 0 {
		// DBの最大連番からカウンターを初期化
		var maxID int64
		err := db.Get(&maxID, "SELECT COALESCE(MAX(CAST(SUBSTRING(display_id FROM 3) AS INTEGER)), 0) FROM videos")
		if err != nil {
			log.Fatalf("最大動画IDの取得に失敗: %v", err)
		}
		rdb.Set(ctx, "video_counter", maxID, 0)
		log.Printf("Redisカウンターを %d で初期化しました。", maxID)
	}
}

// --- メイン関数 ---

func main() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/watch/", watchHandler)
	http.HandleFunc("/upload", uploadHandler)

	log.Println("Webサーバーを開始します: http://localhost")
	if err := http.ListenAndServe(":80", nil); err != nil {
		log.Fatal(err)
	}
}

// --- ハンドラ関数 ---

func indexHandler(w http.ResponseWriter, r *http.Request) {
	var videos []Video
	//ステータスが'active'の動画のみ取得
	err := db.Select(&videos, "SELECT * FROM videos WHERE status = 'active' ORDER BY created_at DESC")
	if err != nil {
		log.Printf("動画一覧の取得に失敗: %v", err)
		http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		return
	}
	templates.ExecuteTemplate(w, "index.html", videos)
}

func watchHandler(w http.ResponseWriter, r *http.Request) {
	displayID := strings.TrimPrefix(r.URL.Path, "/watch/")
	var video Video
	// DisplayIDで動画を検索
	err := db.Get(&video, "SELECT * FROM videos WHERE display_id = $1 AND status = 'active'", displayID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "指定された動画は見つかりません。", http.StatusNotFound)
		} else {
			http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		}
		return
	}
	templates.ExecuteTemplate(w, "video.html", &video)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderUploadPage(w, "", "")
		return
	}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 500*1024*1024) // 500MB制限
		if err := r.ParseMultipartForm(500 * 1024 * 1024); err != nil {
			renderUploadPage(w, "error", "ファイルが大きすぎます（500MBまで）。")
			return
		}
		file, _, err := r.FormFile("video")
		if err != nil {
			renderUploadPage(w, "error", "動画ファイルの取得に失敗しました。")
			return
		}
		defer file.Close()

		title := r.FormValue("title")
		description := r.FormValue("description")
		if title == "" {
			renderUploadPage(w, "error", "タイトルは必須です。")
			return
		}

		uuid := uuid.New().String()
		tempDir := filepath.Join("static", "temp")
		os.MkdirAll(tempDir, os.ModePerm)
		tempVideoPath := filepath.Join(tempDir, uuid+".mp4")

		dst, err := os.Create(tempVideoPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		newCount, _ := rdb.Incr(ctx, "video_counter").Result()
		displayID := "tm" + strconv.FormatInt(newCount, 10)

		video := Video{
			UUID:        uuid,
			DisplayID:   displayID,
			Title:       title,
			Description: sql.NullString{String: description, Valid: description != ""},
			Status:      "processing",
		}
		query := `INSERT INTO videos (uuid, display_id, title, description, status) VALUES (:uuid, :display_id, :title, :description, :status)`
		_, err = db.NamedExec(query, &video)
		if err != nil {
			log.Printf("DBへの動画情報登録に失敗: %v", err)
			http.Error(w, "サーバーエラー", http.StatusInternalServerError)
			return
		}

		job := EncodingJob{UUID: uuid, TempPath: tempVideoPath}
		jobData, _ := json.Marshal(job)
		if err := rdb.LPush(ctx, redisJobQueue, jobData).Err(); err != nil {
			log.Printf("Redisへのジョブ投入に失敗: %v", err)
			http.Error(w, "サーバーエラー", http.StatusInternalServerError)
			return
		}

		log.Printf("ジョブをRedisに投入しました: %s", displayID)
		renderUploadPage(w, "success", "アップロードを受け付けました！変換には数分かかることがあります。")
	}
}

func renderUploadPage(w http.ResponseWriter, status, message string) {
	data := uploadPageData{Status: status, Message: message}
	templates.ExecuteTemplate(w, "upload.html", data)
}
