package services

import (
	"fmt"
	"strings"
)

// The server installs its shared AES-GCM codec before connector initialization.
// Slack credentials use an operator scope and are never workflow secrets.
var slackCredentialEncrypt func(string) (string, error)
var slackCredentialDecrypt func(string) (string, error)

func ConfigureSlackCredentialCodec(encrypt, decrypt func(string) (string, error)) {
	slackCredentialEncrypt, slackCredentialDecrypt = encrypt, decrypt
}
func encodeSlackCredential(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if slackCredentialEncrypt == nil {
		return "", fmt.Errorf("Slack credential encryption unavailable")
	}
	encrypted, err := slackCredentialEncrypt(value)
	return "encrypted:v1:" + encrypted, err
}
func decodeSlackCredential(value string) (string, error) {
	if !strings.HasPrefix(value, "encrypted:v1:") {
		return value, nil
	}
	if slackCredentialDecrypt == nil {
		return "", fmt.Errorf("Slack credential decryption unavailable")
	}
	return slackCredentialDecrypt(strings.TrimPrefix(value, "encrypted:v1:"))
}
func decryptSlackConfig(cfg *SlackConfig) (*SlackConfig, error) {
	var err error
	cfg.BotToken, err = decodeSlackCredential(cfg.BotToken)
	if err != nil {
		return nil, err
	}
	cfg.AppToken, err = decodeSlackCredential(cfg.AppToken)
	if err != nil {
		return nil, err
	}
	for i, conn := range cfg.Connections {
		if cfg.Connections[i], err = decryptSlackConnection(conn); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}
