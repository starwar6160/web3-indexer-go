package engine

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
)

type SigningMiddleware struct {
	PrivateKey ed25519.PrivateKey
	KeyID      string
	PublicKey  ed25519.PublicKey
}

func NewSigningMiddleware(seedHex, keyID string) (*SigningMiddleware, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		// 如果没有提供有效的 seed，我们生成一个临时的（仅用于演示）
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		return &SigningMiddleware{
			PrivateKey: priv,
			PublicKey:  pub,
			KeyID:      keyID + "-temp",
		}, nil
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to cast public key to ed25519.PublicKey")
	}

	return &SigningMiddleware{
		PrivateKey: priv,
		PublicKey:  pub,
		KeyID:      keyID,
	}, nil
}

// responseWrapper 用于捕获响应体进行加签
type responseWrapper struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	return rw.body.Write(b)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}

func (sm *SigningMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过 WebSocket 和 Metrics
		if r.URL.Path == "/ws" || r.URL.Path == "/metrics" || r.URL.Path == "/security" {
			next.ServeHTTP(w, r)
			return
		}

		rw := &responseWrapper{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		// 只有在成功响应时才进行加签
		if rw.statusCode >= 200 && rw.statusCode < 300 {
			data := rw.body.Bytes()
			signature := ed25519.Sign(sm.PrivateKey, data)
			sigBase64 := base64.StdEncoding.EncodeToString(signature)

			// 【关键修复】：必须在发送任何内容之前设置 Header
			w.Header().Set("X-Payload-Signature", sigBase64)
			w.Header().Set("X-Signer-ID", sm.KeyID)
			w.Header().Set("X-Content-Integrity", "Ed25519")

			// 手动触发写入状态码和之前捕获的数据
			w.WriteHeader(rw.statusCode)
			w.Write(data)
		} else {
			// 失败请求原样写回
			w.WriteHeader(rw.statusCode)
			w.Write(rw.body.Bytes())
		}
	})
}

// 自动获取或初始化 Seed
func GetORInitSeed() string {
	seed := os.Getenv("API_SIGNER_SEED")
	if seed == "" {
		// 默认一个固定 Seed 用于演示，确保多次启动签名一致
		return "414f357a429bfc346b7bf5c18c55aaee9b1057e8cc44efb68dfaa7c5ead23a01"
	}
	return seed
}
