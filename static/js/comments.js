document.addEventListener('DOMContentLoaded', function () {
    // --- 1. 変数とCanvasの準備 ---
    const canvas = document.getElementById('comments-canvas');
    const ctx = canvas.getContext('2d');

    const DURATION_SECONDS = 6; // コメントが画面を横切る秒数
    const COMMENT_TEXT = 'テキスト';

    let player_flag;

    const video = document.getElementById('tecuvideo-player');
    video.addEventListener('play', () => {
        player_flag = true;
    });
    video.addEventListener('pause', () => {
        player_flag = false;
    });

    let commentX;
    let speedPerSecond; // 1秒あたりの移動ピクセル数
    let lastTime = 0;   // 前回のフレームの時刻を保存する変数

    // --- 2. 初期化処理 ---
    function initialize() {
        ctx.font = "bold 70px 'Meiryo', sans-serif";
        ctx.fillStyle = 'white';
        ctx.strokeStyle = '#00000070';
        ctx.lineWidth = 10; 


        const commentWidth = ctx.measureText(COMMENT_TEXT).width;
        const totalDistance = canvas.width + commentWidth;
        
        // 1秒あたりに移動すべきピクセル数を計算
        speedPerSecond = totalDistance / DURATION_SECONDS;
        
        // コメントの初期位置を設定
        commentX = canvas.width;

        // アニメーションループを開始
        requestAnimationFrame(animationLoop);
    }

    // --- 3. アニメーションループ ---
    // ループ関数が、ブラウザからタイムスタンプ(currentTime)を受け取るようにする
    function animationLoop(currentTime) {
        // 最初のフレームではlastTimeが0なので、deltaTimeの計算をスキップ
        if (lastTime === 0) {
            lastTime = currentTime;
            requestAnimationFrame(animationLoop);
            return;
        }

        // ① 前回フレームからの経過時間（秒単位）を計算
        const deltaTime = (currentTime - lastTime) / 1000;

        // ② Canvas全体を綺麗にする
        ctx.clearRect(0, 0, canvas.width, canvas.height);

        // ③ 経過時間を使って、今回のフレームで動かすべき距離を計算
        if ( player_flag == true ){
            const moveDistance = speedPerSecond * deltaTime;
            commentX -= moveDistance;
        }

        // ④ 新しい位置にコメントを描画する
        ctx.strokeText(COMMENT_TEXT, commentX, 100);
        ctx.fillText(COMMENT_TEXT, commentX, 100);

        // ⑤ コメントが画面外に出たらリセット
        const commentWidth = ctx.measureText(COMMENT_TEXT).width;
        if (commentX < -commentWidth) {
            commentX = canvas.width;
        }
        
        // ⑥ 次のフレームのために、今回の時刻を保存
        lastTime = currentTime;

        // ⑦ 次の描画タイミングで、再びこの関数自身を呼び出す
        requestAnimationFrame(animationLoop);
    }


    
    // --- 4. 実行開始 ---
    initialize();
});
