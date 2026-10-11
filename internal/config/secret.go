package config

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const secretService = "godrome"

func secretAccount(server, username string) string {
	return strings.TrimSpace(server) + "\n" + strings.TrimSpace(username)
}

// SavePassword stores the password in the operating-system keyring.
// Linux uses the Secret Service, macOS the login keychain, and Windows
// Credential Manager. The JSON settings file never receives the password.
func SavePassword(server, username, password string) error {
	if err := keyring.Set(secretService, secretAccount(server, username), password); err != nil {
		return errors.New("无法保存密码，请确认系统密钥环可用")
	}
	return nil
}

// LoadPassword reads a password saved for this server and username.
// A missing entry is not an error.
func LoadPassword(server, username string) (string, bool, error) {
	password, err := keyring.Get(secretService, secretAccount(server, username))
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && password == "") {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.New("无法读取已保存的密码")
	}
	return password, true, nil
}

// DeletePassword removes a saved password. A missing entry is success.
func DeletePassword(server, username string) error {
	err := keyring.Delete(secretService, secretAccount(server, username))
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return errors.New("无法清除已保存的密码")
}
