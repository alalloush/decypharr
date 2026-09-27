package config

// SecretFields lists the config.json properties that hold credentials, by Go
// type name (types in this package) and JSON property name. The generated
// schema marks them writeOnly, and GET /api/config never returns their
// values. A new credential field must be added here.
var SecretFields = map[string][]string{
	"Config":         {"session_secret", "discord_webhook_url"},
	"Arr":            {"token"},
	"Debrid":         {"api_key", "download_api_keys", "rc_pass"},
	"ExternalRclone": {"rc_password"},
	"Notifications":  {"webhook_url"},
	"SMB":            {"password"},
	"Strm":           {"secret"},
	"UsenetProvider": {"password"},
}
