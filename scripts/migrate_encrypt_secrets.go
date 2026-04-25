//go:build ignore
// +build ignore

// 数据迁移脚本：将现有云账号的 AccessKeySecret 从明文加密为密文
// 使用方法: go run scripts/migrate_encrypt_secrets.go -key "your-encryption-key"
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoURI      = flag.String("mongo", "mongodb://localhost:27017", "MongoDB URI")
	database      = flag.String("db", "ecam", "Database name")
	encryptionKey = flag.String("key", "", "Encryption key (16/24/32 bytes, required)")
	dryRun        = flag.Bool("dry-run", false, "Dry run mode, don't actually update")
)

func main() {
	flag.Parse()

	if *encryptionKey == "" {
		log.Fatal("encryption key is required, use -key flag")
	}

	// 初始化加密器
	crypto, err := NewAESCrypto(*encryptionKey)
	if err != nil {
		log.Fatalf("failed to create crypto: %v", err)
	}

	// 连接 MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// MongoDB连接配置
	credential := options.Credential{
		Username:   "ecmdb",
		Password:   "123456",
		AuthSource: "admin",
	}
	clientOpts := options.Client().
		ApplyURI(*mongoURI).
		SetAuth(credential)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database(*database).Collection("cloud_accounts")

	// 查询所有云账号
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("failed to find accounts: %v", err)
	}
	defer cursor.Close(ctx)

	var updated, skipped, failed int

	for cursor.Next(ctx) {
		var account bson.M
		if err := cursor.Decode(&account); err != nil {
			log.Printf("failed to decode account: %v", err)
			failed++
			continue
		}

		id := account["id"]
		secret, ok := account["access_key_secret"].(string)
		if !ok || secret == "" {
			log.Printf("account %v: no secret, skipping", id)
			skipped++
			continue
		}

		// 检查是否已经是加密的 (尝试解密)
		if _, err := crypto.Decrypt(secret); err == nil {
			log.Printf("account %v: already encrypted, skipping", id)
			skipped++
			continue
		}

		// 加密
		encrypted, err := crypto.Encrypt(secret)
		if err != nil {
			log.Printf("account %v: failed to encrypt: %v", id, err)
			failed++
			continue
		}

		if *dryRun {
			log.Printf("account %v: would encrypt (dry-run)", id)
			updated++
			continue
		}

		// 更新数据库
		_, err = collection.UpdateOne(ctx,
			bson.M{"id": id},
			bson.M{"$set": bson.M{
				"access_key_secret": encrypted,
				"update_time":       time.Now(),
				"utime":             time.Now().Unix(),
			}},
		)
		if err != nil {
			log.Printf("account %v: failed to update: %v", id, err)
			failed++
			continue
		}

		log.Printf("account %v: encrypted successfully", id)
		updated++
	}

	fmt.Printf("\n=== Migration Summary ===\n")
	fmt.Printf("Updated: %d\n", updated)
	fmt.Printf("Skipped: %d\n", skipped)
	fmt.Printf("Failed:  %d\n", failed)
	if *dryRun {
		fmt.Println("(dry-run mode, no actual changes)")
	}
}

// AESCrypto AES 加密器
type AESCrypto struct {
	key []byte
}

// NewAESCrypto 创建 AES 加密器
func NewAESCrypto(key string) (*AESCrypto, error) {
	keyBytes := []byte(key)
	keyLen := len(keyBytes)

	var finalKey []byte
	switch {
	case keyLen >= 32:
		finalKey = keyBytes[:32]
	case keyLen >= 24:
		finalKey = keyBytes[:24]
	case keyLen >= 16:
		finalKey = keyBytes[:16]
	default:
		finalKey = make([]byte, 16)
		copy(finalKey, keyBytes)
	}

	return &AESCrypto{key: finalKey}, nil
}

// Encrypt 加密字符串
func (c *AESCrypto) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

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

// Decrypt 解密字符串
func (c *AESCrypto) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
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
		return "", errors.New("invalid ciphertext")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
