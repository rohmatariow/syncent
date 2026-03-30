package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

type CryptoService struct {
	key []byte
}

func NewCryptoService(masterKey []byte) *CryptoService {
	return &CryptoService{key: masterKey}
}

func (c *CryptoService) GenerateEd25519Keypair() (string, string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("key generation failed: %w", err)
	}

	sshPriv, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := string(pem.EncodeToMemory(sshPriv))

	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal public key: %w", err)
	}
	pubSSH := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))

	return privPEM, pubSSH, nil
}

func (c *CryptoService) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c *CryptoService) Decrypt(encrypted string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed (wrong master key?): %w", err)
	}
	return string(plaintext), nil
}

func (c *CryptoService) ParsePrivateKey(encryptedKey string) (ssh.Signer, error) {
	pemStr, err := c.Decrypt(encryptedKey)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(pemStr))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return signer, nil
}
