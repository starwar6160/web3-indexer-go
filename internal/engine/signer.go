package engine

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
)

type SigningMiddleware struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	KeyID      string
}

func NewSigningMiddleware(seedHex string, keyID string) (*SigningMiddleware, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != 32 {
		// 如果没有提供有效的 seed，我们生成一个临时的（仅用于演示）
		pub, priv, _ := ed25519.GenerateKey(nil)
		return &SigningMiddleware{
			PrivateKey: priv,
			PublicKey:  pub,
			KeyID:      keyID + "-temp",
		}, nil
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

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
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (sm *SigningMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过非 API 或静态资源请求（可选）
		if r.URL.Path == "/ws" || r.URL.Path == "/metrics" {
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
			signature := ed25519.Sign(sm.PrivateKey, rw.body.Bytes())
			sigBase64 := base64.StdEncoding.EncodeToString(signature)

			// 注入安全响应头
			w.Header().Set("X-Payload-Signature", sigBase64)
			w.Header().Set("X-Signer-ID", sm.KeyID)
			w.Header().Set("X-Content-Integrity", "Ed25519")
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
