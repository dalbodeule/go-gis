//go:build qt

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gogis/ui/qt/native"
)

const postGISDSNEnvironment = "GOGIS_POSTGIS_DSN"

type postGISConnectionForm struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
	SSLMode  string `json:"sslMode"`
}

func desktopPostGISDSN(payload, fallback string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(fallback) == "" {
			return "", fmt.Errorf("enter PostgreSQL connection details or set %s", postGISDSNEnvironment)
		}
		return strings.TrimSpace(fallback), nil
	}
	var form postGISConnectionForm
	if err := json.Unmarshal([]byte(payload), &form); err != nil {
		return "", fmt.Errorf("invalid PostgreSQL connection details")
	}
	host, database, user := strings.TrimSpace(form.Host), strings.TrimSpace(form.Database), strings.TrimSpace(form.User)
	if host == "" || database == "" || user == "" {
		return "", fmt.Errorf("PostgreSQL host, database, and user are required")
	}
	port := strings.TrimSpace(form.Port)
	if port == "" {
		port = "5432"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("PostgreSQL port must be between 1 and 65535")
	}
	sslMode := strings.TrimSpace(form.SSLMode)
	if sslMode == "" {
		sslMode = "prefer"
	}
	switch sslMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return "", fmt.Errorf("unsupported PostgreSQL SSL mode")
	}
	account := url.User(user)
	if form.Password != "" {
		account = url.UserPassword(user, form.Password)
	}
	connection := url.URL{Scheme: "postgres", User: account, Host: net.JoinHostPort(host, port), Path: "/" + database}
	connection.RawQuery = url.Values{"sslmode": {sslMode}}.Encode()
	return connection.String(), nil
}

func currentDesktopPostGISDSN() (string, error) {
	return desktopPostGISDSN(native.CurrentPostGISConnection(), os.Getenv(postGISDSNEnvironment))
}

func postGISConnectionError(err error, dsn string) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if dsn != "" {
		message = strings.ReplaceAll(message, dsn, "[PostgreSQL connection]")
	}
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.User != nil {
		if password, found := parsed.User.Password(); found && password != "" {
			message = strings.ReplaceAll(message, password, "[redacted]")
		}
	}
	return fmt.Errorf("%s", message)
}
