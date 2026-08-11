// [第一阶段优化-测试] 覆盖 bcrypt、旧 MD5 迁移、安全随机数和文件限制。
package utils

import (
	"mime/multipart"
	"regexp"
	"testing"
)

func TestPasswordHashAndLegacyUpgrade(t *testing.T) {
	const password = "correct-horse-battery-staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if valid, upgrade := VerifyPassword(hash, password); !valid || upgrade {
		t.Fatalf("bcrypt password verification = (%v, %v), want (true, false)", valid, upgrade)
	}
	if valid, _ := VerifyPassword(hash, "wrong-password"); valid {
		t.Fatal("VerifyPassword() accepted a wrong password")
	}

	legacyHash := legacyMD5(password)
	if valid, upgrade := VerifyPassword(legacyHash, password); !valid || !upgrade {
		t.Fatalf("legacy password verification = (%v, %v), want (true, true)", valid, upgrade)
	}
}

func TestGetRandomNumbers(t *testing.T) {
	code := GetRandomNumbers(32)
	if !regexp.MustCompile(`^[0-9]{32}$`).MatchString(code) {
		t.Fatalf("GetRandomNumbers() = %q, want 32 digits", code)
	}
}

func TestValidateFile(t *testing.T) {
	for _, test := range []struct {
		name    string
		file    *multipart.FileHeader
		wantErr bool
	}{
		{name: "markdown", file: &multipart.FileHeader{Filename: "notes.md", Size: 128}},
		{name: "empty", file: &multipart.FileHeader{Filename: "notes.txt", Size: 0}, wantErr: true},
		{name: "too large", file: &multipart.FileHeader{Filename: "notes.txt", Size: 5<<20 + 1}, wantErr: true},
		{name: "unsupported extension", file: &multipart.FileHeader{Filename: "notes.pdf", Size: 128}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateFile(test.file)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateFile() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
