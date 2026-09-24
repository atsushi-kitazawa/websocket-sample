# WebSocket 生TCP実装サンプル (Go)

> **Developed with Antigravity**  
> 本プロジェクトは、Google DeepMind の AI コーディングアシスタント **Antigravity** を活用したペアプログラミングによって実装されました。プロトコル設計・タスク計画から Nix 開発環境の構築、TDD による RFC 6455 準拠コアロジックおよびサーバープッシュ通知の実装まで一貫して行われています。

Go言語の標準ライブラリ（`net`, `crypto/sha1`, `encoding/base64` 等）のみを使用し、サードパーティ製ライブラリを一切使わずに生 TCP ソケットから RFC 6455 準拠の WebSocket サーバーおよびクライアントを実装したサンプルプロジェクトです。

## 特徴
- **外部依存ゼロ**: Go 標準ライブラリのみで構築
- **RFC 6455 完全準拠**:
  - `Sec-WebSocket-Key` と `Sec-WebSocket-Accept` によるオープニングハンドシェイク
  - 可変長フレームヘッダー（0〜125, 126=16bit拡張, 127=64bit拡張）のバイナリパース/シリアライズ
  - クライアント送信時の自動 4 バイト XOR マスク付与およびサーバー側での未マスク検出（プロトコルエラー）
  - Ping / Pong キープアライブフレーム処理
  - ステータスコードと理由を含むクローズハンドシェイク（Graceful Close）
- **ユニットテスト完備**: ハンドシェイク計算、境界値フレームテスト、マスク対称性テスト、異常系テスト
- **Nix 対応**: `flake.nix` により本プロジェクト限定の Go 開発環境が完備

---

## ディレクトリ構成
```text
.
├── flake.nix              # Nix Flake 開発環境定義 (Go 1.26, gopls, golangci-lint)
├── go.mod                 # Go モジュール定義
├── TODO.md                # タスク計画と進捗管理
├── websocket.md           # WebSocket プロトコル詳細解説ガイド
├── pkg/
│   └── websocket/         # WebSocket コアライブラリ
│       ├── handshake.go      # ハンドシェイク処理 (サーバー/クライアント)
│       ├── handshake_test.go # ハンドシェイクのユニットテスト
│       ├── frame.go          # フレーム定義、ReadFrame / WriteFrame / マスク処理
│       ├── frame_test.go     # フレーム処理のユニットテスト
│       ├── hub.go            # 接続管理 (Hub/Client) とブロードキャスト
│       └── hub_test.go       # Hub のユニットテスト
└── cmd/
    ├── server/
    │   └── main.go        # WebSocket サーバーエントリポイント (プッシュ配信対応)
    └── client/
        └── main.go        # WebSocket クライアントエントリポイント
```

---

## 実行方法

### 1. ユニットテストの実行
```bash
nix develop --command go test -v -race ./pkg/websocket/...
```

### 2. サーバーの起動

#### 通常起動 (手動プッシュ・チャットブロードキャスト有効)
```bash
nix develop --command go run ./cmd/server/main.go -port 8080
```
- サーバーのターミナルで文字を入力して Enter を押すと、接続中の全クライアントへ即座に一斉プッシュ通知（`[Server Notice]: ...`）されます。

#### 自動定期プッシュ付きで起動 (例: 3秒おき)
```bash
nix develop --command go run ./cmd/server/main.go -port 8080 -push-interval 3s
```
- クライアント側から何もリクエストを送らなくても、3秒ごとに現在時刻と接続クライアント数が自動プッシュ配信されます。

### 3. クライアントの実行

#### 対話モード (インタラクティブ CLI)
複数のターミナルを開いて同時に起動できます：
```bash
nix develop --command go run ./cmd/client/main.go -addr localhost:8080
```
- 通常のテキストを入力して Enter: メッセージを送信（他の接続クライアント全員へリアルタイム配信）
- `/ping`: Ping フレームを送信し、サーバーから Pong を受信
- `/close`: Close フレームを送信し、正常切断

#### 単発メッセージ送信モード
```bash
nix develop --command go run ./cmd/client/main.go -addr localhost:8080 -msg "Hello, WebSocket!"
```

---

## 開発プロセス (Developed with Antigravity)

本プロジェクトの設計・実装・テスト・ドキュメント作成は、AI コーディングアシスタント **Antigravity** を用いた段階的なペアプログラミングによって行われました。

1. **仕様整理とタスク計画 ([TODO.md](TODO.md))**:
   - サードパーティ製ライブラリを排除し、Go 標準ライブラリのみで RFC 6455 を実装するためのフェーズ別計画を策定。
2. **プロトコル解説ドキュメントの生成 ([websocket.md](websocket.md))**:
   - HTTP Upgrade ハンドシェイクの仕組み、バイナリフレームのビット構造、セキュリティ上のマスク処理の必要性（キャッシュポイズニング防止）などを体系的にドキュメント化。
3. **Nix Flake による環境構築 ([flake.nix](flake.nix))**:
   - ホスト環境を汚さない独立した Go 開発環境（Go 1.26, gopls, golangci-lint）を構築。
4. **ユニットテスト駆動の実装**:
   - RFC 6455 既知のテストベクタによるハンドシェイク検証、可変長ペイロード（125/126/127）の境界値テスト、マスク対称性テストを実装し、全テスト合格を確認しながらコア処理を構築。
5. **機能追加（サーバープッシュ通知 & Hub パターン）**:
   - ゴルーチン競合を防ぐスレッドセーフな `Hub` および `Client.WritePump` を導入し、手動コンソールプッシュ・定期 Ticker プッシュ・チャット型ブロードキャストを実現。

