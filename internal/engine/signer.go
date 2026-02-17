package engine

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
)

// SignedPayload 封装带签名的 JSON 数据包 (信封模式)
type SignedPayload struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Signature string      `json:"signature"`
	PubKey    string      `json:"pub_key"`
	SignerID  string      `json:"signer_id"`
}

// SignerMachine 负责 Ed25519 签名逻辑
type SignerMachine struct {
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
	signerID string
	mu      sync.RWMutex
}

// NewSignerMachine 创建签名机
// seed 用于确定性生成密钥（可选），如果为 nil 则随机生成
func NewSignerMachine(signerID string) *SignerMachine {
	// 在演示模式下，我们随机生成一对密钥
	// 生产环境下应从安全存储加载
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		slog.Error("failed_to_generate_ed25519_key", "err", err)
		return nil
	}

	return &SignerMachine{
		privKey:  priv,
		pubKey:   pub,
		signerID: signerID,
	}
}

// Sign 签署数据并返回信封
func (s *SignerMachine) Sign(msgType string, data interface{}) (*SignedPayload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// 执行 Ed25519 签名
	sig := ed25519.Sign(s.privKey, msgBytes)

	return &SignedPayload{
		Type:      msgType,
		Data:      data,
		Signature: hex.EncodeToString(sig),
		PubKey:    hex.EncodeToString(s.pubKey),
		SignerID:  s.signerID,
	}, nil
}

// GetPublicKeyHex 返回公钥的十六进制表示
func (s *SignerMachine) GetPublicKeyHex() string {
	return hex.EncodeToString(s.pubKey)
}

// GetSignerID 返回签名者标识
func (s *SignerMachine) GetSignerID() string {
	return s.signerID
}