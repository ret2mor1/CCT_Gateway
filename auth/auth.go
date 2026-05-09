package auth

import (
	"cct/config"
	"net/http"
	"strings"
)

func ExtractAPIKey(r *http.Request, auth config.AuthConfig) string {
	var val string
	if auth.Location == "header" {
		val = r.Header.Get(auth.KeyName)
	}

	if auth.Prefix != "" && strings.HasPrefix(val, auth.Prefix+" ") {
		val = strings.TrimPrefix(val, auth.Prefix+" ")
	}
	return val
}

func IsAuthorized(clientKey string, allowedKeys []string) bool {
	for _, k := range allowedKeys {
		if clientKey == k {
			return true
		}
	}
	return false
}
