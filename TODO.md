# WebSocket 生TCP実装 タスク計画 (TODO)

## 概要
Go言語の標準ライブラリ（`net`, `crypto/sha1`, `encoding/base64` など）のみを使用し、サードパーティライブラリに依存せず生のTCPソケットからRFC 6455に準拠したWebSocketサーバーおよびクライアントを実装する。

---

## タスク一覧

### Phase 0: Nix によるGo開発環境の構築
- [x] Nix Flake 定義の作成 (`flake.nix`)
  - Go, gopls, gotools, golangci-lint の導入
- [x] direnv 連携用設定 (`.envrc`, `.gitignore`)
- [x] 環境動作確認 (`nix develop --command go version`)

### Phase 1: プロジェクト初期化とディレクトリ構成定義
- [x] Goモジュールの初期化 (`go mod init websocket-sample`)
- [x] ディレクトリ構成の構築
  - `pkg/websocket/` : WebSocketプロトコル処理（ハンドシェイク、フレームエンコード/デコード）
  - `cmd/server/` : WebSocket サーバーのエントリポイント
  - `cmd/client/` : WebSocket クライアントのエントリポイント

### Phase 2: WebSocket プロトコルコア処理とユニットテストの実装 (`pkg/websocket`)
- [x] **ハンドシェイク処理の実装 (`handshake.go`)**
  - [x] サーバー側: HTTP Upgrade リクエストパースと `Sec-WebSocket-Key` 抽出
  - [x] サーバー側: `Sec-WebSocket-Accept` の計算（Magic String: `258EAFA5-E914-47DA-95CA-C5AB0DC85B11` + SHA-1 + Base64）
  - [x] サーバー側: 101 Switching Protocols レスポンス送信
  - [x] クライアント側: ランダム `Sec-WebSocket-Key` 生成と HTTP Upgrade リクエスト送信
  - [x] クライアント側: 101 レスポンス受信と `Sec-WebSocket-Accept` 検証
- [x] **ハンドシェイクのユニットテスト実装 (`handshake_test.go`)**
  - [x] `Sec-WebSocket-Accept` 計算の検証（RFC 6455 仕様の既知テストベクタ）
  - [x] HTTP Upgrade リクエスト/レスポンスのパース正常系・異常系テスト
  - [x] `net.Pipe` を用いたハンドシェイク送受信の結合テスト
- [x] **フレーム処理の実装 (`frame.go`)**
  - [x] WebSocketフレーム構造体の定義 (FIN, Opcode, Mask, Payload Length, Masking Key, Payload)
  - [x] フレーム読み込み関数 `ReadFrame(io.Reader)` の実装
    - [x] 1, 2バイト目のヘッダ情報パース
    - [x] 拡張ペイロード長（126/127）のサポート
    - [x] マスクキーの読み込みとアンマスク処理 (`data[i] ^ maskKey[i % 4]`)
  - [x] フレーム書き込み関数 `WriteFrame(io.Writer, frame)` の実装
    - [x] クライアント送信時のマスク処理（ランダムマスクキー付与）
    - [x] サーバー送信時のノーマスク処理
  - [x] コントロールフレーム処理 (Ping, Pong, Close)
- [x] **フレーム処理のユニットテスト実装 (`frame_test.go`)**
  - [x] `WriteFrame` と `ReadFrame` のエンコード/デコード往復テスト（Round-trip）
  - [x] ペイロード長境界値テスト（<=125バイト, 126〜65,535バイトの2バイト拡張, 65,536バイト以上の8バイト拡張）
  - [x] マスク・アンマスク処理の相互変換・整合性テスト
  - [x] コントロールフレーム（Ping, Pong, Close）のパースおよびペイロード長制約（125バイト以下）のテスト
  - [x] 不正フレームに対するエラーハンドリングテスト（不正Opcode、未サポートフラグ等）

### Phase 3: WebSocket サーバーの実装 (`cmd/server/main.go`)
- [x] TCP リスナーの起動 (`net.Listen("tcp", ":8080")`)
- [x] クライアント接続受け入れループの作成
- [x] WebSocket ハンドシェイクの実行
- [x] メッセージ受信・エコー送信ループの実装 (Ping/Pong, Close フレーム対応含む)

### Phase 4: WebSocket クライアントの実装 (`cmd/client/main.go`)
- [x] TCP 接続確立 (`net.Dial("tcp", "localhost:8080")`)
- [x] WebSocket ハンドシェイクの送信・検証
- [x] メッセージの送信とサーバーからの応答（エコー）受信用ループの実装

### Phase 5: テスト実行と動作検証・疎通確認
- [x] 全ユニットテストの実行とパス確認 (`go test -v ./pkg/websocket/...`)
- [x] サーバーとクライアントを起動し、テキストメッセージ送受信の動作検証
- [x] Ping/Pong フレーム、Close フレームの正常性確認

---

## 機能追加計画: サーバープッシュ通知機能

### 概要
WebSocket の強みである「全二重通信」を活かし、サーバー主導で接続中のクライアント（単一・複数）へ自律的にメッセージを能動送信（プッシュ配信）できる仕組みを実装する。
Go のゴルーチンとチャネルを活用し、同一コネクションへの同時書き込み競合を防止するスレッドセーフな `Hub`（接続マネージャー）を設計する。

### Phase 6: クライアント接続管理 (Hub) の設計・実装 (`pkg/websocket`)
- [x] **Hub 構造体と Client 構造体の実装 (`pkg/websocket/hub.go`)**
  - [x] クライアント登録（Register）/ 解除（Unregister）のスレッドセーフな管理
  - [x] 各クライアントごとの送信キューチャネル（`chan *Frame`）による書き込み競合の分離
  - [x] 全クライアントへの一斉同報配信（Broadcast）メソッドの実装
- [x] **Hub のユニットテスト実装 (`pkg/websocket/hub_test.go`)**
  - [x] 複数クライアントの登録・解除のライフサイクル検証
  - [x] ブロードキャストメッセージが全クライアントに正しく配信されるかの検証
  - [x] 詰まったクライアント（バッファフル）や突然切断時の安全な切断・メモリリーク防止検証

### Phase 7: サーバー側プッシュ通知トリガーの実装 (`cmd/server/main.go`)
- [x] **Hub とサーバー本体の統合**
  - [x] 新規接続時の Hub 登録と、クライアント専用の書き込みゴルーチン（WritePump）起動
- [x] **サーバー CLI からの手動プッシュ通知機能**
  - [x] サーバープロセスの標準入力（stdin）読み取りゴルーチンの実装
  - [x] サーバー側コンソールで入力したメッセージを、全接続クライアントへ即座に一斉ブロードキャスト
- [x] **定期自動プッシュ機能 (Ticker Push - フラグ対応)**
  - [x] `-push-interval` フラグ（例: `-push-interval 5s`）の追加
  - [x] 一定周期で現在時刻や接続クライアント数を全クライアントへ自動通知するタイマーループの実装
- [x] **チャットルーム型ブロードキャスト（他クライアントへの通知）**
  - [x] クライアントから受信したテキストを、接続中の全クライアントへ転送・配信

### Phase 8: 複数クライアントでのプッシュ通知動作検証
- [x] 複数クライアント（Client 1, Client 2）を同時接続
- [x] サーバーコンソールからの手動入力が、全クライアントの画面に即座にプッシュ表示されることを確認
- [x] 定期自動プッシュ（時刻・接続数）が全クライアントに届くことを確認
- [x] 1つのクライアントを切断しても、残りのクライアントへ問題なくプッシュ配信が継続されることを確認
