package apisubscriptions

import (
	"crypto/sha256"
	"encoding/hex"
)

// CredentialSchemaFingerprint 返回固定凭据布局的兼容指纹，不读取密钥或数据库。
func CredentialSchemaFingerprint() string {
	hash := sha256.New()
	for _, object := range credentialSchemaObjects {
		_, _ = hash.Write([]byte(object.ObjectType + "\x00" + object.Name + "\x00" + object.Statement + "\x00"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
