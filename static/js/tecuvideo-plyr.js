document.addEventListener('DOMContentLoaded', function () {
    window.tecuVideoPlayer = { version: '1.0' };

    // --- HTML要素の取得 ---
    const video = document.getElementById('tecuvideo-player');
    const playerContainer = document.querySelector('.custom-video-player');
    const playPauseBtn = document.getElementById('play-pause-btn');
    const playPauseBtnIcon = document.getElementById('play-btn');
    const seekBar = document.getElementById('seek-bar');
    const seekBarContainer = document.querySelector('.seek-bar-container'); 
    const currentTimeEl = document.getElementById('current-time');
    const durationEl = document.getElementById('duration');
    const muteBtn = document.getElementById('mute-btn');
    const volIcon = document.getElementById('volume-icon');
    const volumeBar = document.getElementById('volume-bar');
    const qualityBtn = document.getElementById('quality-btn');
    const qualityMenu = document.getElementById('quality-lists'); // IDを修正
    const fullscreenBtn = document.getElementById('fullscreen-btn');
    const fullScrIcon = document.getElementById('fullScr-icon');
    const canvas = document.getElementById('comments-canvas');

    // シークバー操作時の自動再生判定変数
    let vidPlayer_toggle = false;
    let isSeeking = false;

    // ---読み込み時にボリュームを前回と同じ数値にする
    if ( Cookies.get('tecuvideo_volume') !== undefined ){ video.volume = Cookies.get('tecuvideo_volume') }
    handleVolume()
    updatePlayPauseIcon()



    // --- HLSの初期化 ---
    const m3u8Url = playerContainer.dataset.videoSrc;
    let hls = null;

    if (Hls.isSupported()) {
        hls = new Hls();
        hls.loadSource(m3u8Url);
        hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = m3u8Url;
    }

    // --- イベントリスナーの登録 ---
    playPauseBtn.addEventListener('click', togglePlayPause);
    video.addEventListener('play', updatePlayPauseIcon);
    video.addEventListener('pause', updatePlayPauseIcon);
    video.addEventListener('loadedmetadata', initializePlayer);
    video.addEventListener('timeupdate', updateTimeAndSeekBar);
    seekBar.addEventListener('input', handleSeekBarInput); // ドラッグ中の処理
    seekBar.addEventListener('change', handleSeekBarChange); // ドラッグ終了後の処理
    seekBarContainer.addEventListener('mousedown', () => { isSeeking = true });
    seekBarContainer.addEventListener('mouseup', () => { isSeeking = false; video.play(); });
    muteBtn.addEventListener('click', toggleMute);
    volumeBar.addEventListener('input', handleVolume);
    video.addEventListener('volumechange', updateVolumeUI);
    fullscreenBtn.addEventListener('click', toggleFullscreen);
    qualityBtn.addEventListener('click', toggleQualityMenu);
    document.addEventListener('visibilitychange', handleVisibilityChange);
    canvas.addEventListener('click', () =>{
        if (video.paused || video.ended) { 
            video.play();
            vidPlayer_toggle = true;
        } else {
            vidPlayer_toggle = false; 
            video.pause(); 
        }
    })

    if (hls) {
        hls.on(Hls.Events.MANIFEST_PARSED, function (event, data) {
            setupQualityMenu(data.levels);
        });
    }

    // --- 機能ごとの関数 ---
    function togglePlayPause() {
        if (video.paused || video.ended) { 
            video.play();
            vidPlayer_toggle = true;
        } else {
            vidPlayer_toggle = false; 
            video.pause(); 
        }
    }
    function updatePlayPauseIcon() {
        playPauseBtnIcon.src = video.paused ? '/static/system-images/play.svg' : '/static/system-images/stop.svg';
    }
    function initializePlayer() {
        const duration = video.duration;
        seekBar.max = duration;
        durationEl.textContent = formatTime(duration);
        console.log(duration)
        updateVolumeUI();
    }
    
    function updateTimeAndSeekBar() {

        if (isSeeking) return;

        // isNaNチェックで、durationが取得できていない場合のエラーを防ぐ
        if (video.duration && !isNaN(video.duration)) {
            // 操作用スライダーの位置を更新
            seekBar.value = video.currentTime;
            // 見た目用のプログレスバー(div)を取得
            const progressBar = document.getElementById('seek-progress');
            // 再生済みの割合を計算
            const progressPercent = (video.currentTime / video.duration) * 100;
            // divの幅を更新
            if (progressBar) {
                progressBar.style.width = progressPercent + '%';
            }
        }
        // 時間表示は常に更新
        currentTimeEl.textContent = formatTime(video.currentTime);
    }

    // ドラッグ中は、見た目のUIだけを更新
    function handleSeekBarInput() {
        video.pause();
        // isNaNチェック
        if (video.duration && !isNaN(video.duration)) {
            // 見た目用のプログレスバー(div)を取得
            const progressBar = document.getElementById('seek-progress');
            // 再生済みの割合を計算
            const progressPercent = (seekBar.value / video.duration) * 100;
                // divの幅を更新
            if (progressBar) {
                progressBar.style.width = progressPercent + '%';
            }
        }
        // 時間のテキスト表示も更新
        currentTimeEl.textContent = formatTime(seekBar.value);
    }

    // ドラッグが終わったら、動画の再生位置を実際に変更
    function handleSeekBarChange() {
        if ( vidPlayer_toggle ){ video.play(); }
        
        video.currentTime = seekBar.value;
    }

    function toggleMute() {
        video.muted = !video.muted;
        if ( video.muted ){
            volIcon.src = "/static/system-images/v-zero.svg"
        } else {
            if (video.volume >= 0.5){
                volIcon.src = "/static/system-images/v-max.svg"
            } else if ( video.volume > 0 ){
                volIcon.src = "/static/system-images/v-half.svg"
            }
        }
    }
    function handleVolume() {
        video.volume = volumeBar.value;
        Cookies.set('tecuvideo_volume', video.volume);
        if (video.volume >= 0.5 && !video.muted){
            volIcon.src = "/static/system-images/v-max.svg"
        } else if ( video.volume > 0 && !video.muted){
            volIcon.src = "/static/system-images/v-half.svg"
        } else {
            volIcon.src = "/static/system-images/v-zero.svg"
        }
    }
    
    function updateVolumeUI() {
        // スライダーのつまみの位置を更新
        volumeBar.value = video.volume;
        // 音量の割合（パーセント）を計算
        const volumePercent = video.volume * 100;
        // CSS変数の値を更新して、バーの色を動かす
        volumeBar.style.setProperty('--volume-progress', volumePercent + '%');
    }

    function toggleFullscreen() {
        if (!document.fullscreenElement) {
            fullScrIcon.src = "/static/system-images/minScr.svg"
            playerContainer.requestFullscreen().catch(err => {
                alert(`全画面表示にできませんでした: ${err.message}`);
            });
        } else { 
            document.exitFullscreen();
            fullScrIcon.src = "/static/system-images/fullScr.svg"
        }

    }
    function toggleQualityMenu() {
        qualityMenu.classList.toggle('hidden');
    }
    function setupQualityMenu(levels) {
        const autoLevel = { height: 'Auto' };
        const availableLevels = [autoLevel, ...levels];
        qualityMenu.innerHTML = '';
        availableLevels.forEach((level, index) => {
            const btn = document.createElement('button');
            btn.textContent = `${level.height}${level.height !== 'Auto' ? 'p' : ''}`;
            btn.onclick = () => {
                if (hls) { hls.currentLevel = index - 1; }
                toggleQualityMenu();
            };
            qualityMenu.appendChild(btn);
        });
    }

    // スマホ版の動画再生不具合の修正
    function handleVisibilityChange(){
        if (document.hidden || !hls){
            return;
        }

        try {
            hls.recoverMediaError();
        } catch (e) {
            console.error("cant recover");
        }
    }

    // ★重複していたformatTime関数を一つに修正
    function formatTime(seconds) {
        const minutes = Math.floor(seconds / 60);
        const secs = Math.floor(seconds % 60);
        return `${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
    }
});
