package server

import (
	"cmp"
	stdjson "encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/sirrobot01/decypharr/internal/config"
)

// secretPlaceholder stands in for a stored credential in GET /api/config.
// Posting it back, as the settings page does for a field the user left alone,
// keeps the stored value. Sonarr and Radarr use the same convention.
const secretPlaceholder = "********"

var configPkgPath = reflect.TypeFor[config.Config]().PkgPath()

// redactSecrets replaces every non-empty config.SecretFields value in c with
// secretPlaceholder. c must be a private copy: its slices are edited in place.
func redactSecrets(c *config.Config) {
	eachSecret(reflect.ValueOf(c).Elem(), func(_, _ string, v reflect.Value) {
		secretStrings(v, func(s reflect.Value) {
			if s.String() != "" {
				s.SetString(secretPlaceholder)
			}
		})
	})
}

// secretStrings calls fn with a secret field's string, or with each string of
// a []string secret.
func secretStrings(v reflect.Value, fn func(reflect.Value)) {
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			fn(v.Index(i))
		}
		return
	}
	fn(v)
}

// eachSecret calls fn with the type name, JSON name and value of every
// config.SecretFields field reachable from v. Maps are not walked; no secret
// lives in one.
func eachSecret(v reflect.Value, fn func(typeName, field string, v reflect.Value)) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			eachSecret(v.Elem(), fn)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			eachSecret(v.Index(i), fn)
		}
	case reflect.Struct:
		t := v.Type()
		if t.PkgPath() != configPkgPath {
			return
		}
		secrets := config.SecretFields[t.Name()]
		for i := range t.NumField() {
			field := t.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if !field.IsExported() || name == "-" {
				continue
			}
			if slices.Contains(secrets, name) {
				fn(t.Name(), name, v.Field(i))
			} else {
				eachSecret(v.Field(i), fn)
			}
		}
	}
}

// restoreSecrets puts the stored credentials back into next, the merge of a
// POST /api/config body onto stored:
//   - a secret posted as secretPlaceholder keeps the stored value;
//   - so does a secret left out of a posted debrid, Arr or usenet provider.
//     The merge replaces those lists whole, so the stored entry with the same
//     identity supplies it: debrid and Arr by name, usenet provider by host,
//     port and username;
//   - an explicit "" (or [] for download_api_keys) clears the secret.
//
// A kept secret must not follow its entry somewhere new. If a debrid's
// provider, api_host or proxy, an Arr's host or an rclone rc_url changed to a
// new value, the secret has to be sent again; otherwise anyone who can save
// settings could point a stored key they cannot read at a host of their
// choosing. Clearing api_host or proxy only returns to the provider's own API.
//
// storedArrs is the Arr list GET /api/config returned, which includes the Arrs
// only the Arr service knows about.
func restoreSecrets(stored *config.Config, storedArrs []config.Arr, body []byte, next *config.Config) error {
	var patch map[string]any
	if err := stdjson.Unmarshal(body, &patch); err != nil {
		return err
	}

	keepPlaceholder(&next.DiscordWebhook, stored.DiscordWebhook)
	keepPlaceholder(&next.Notifications.WebhookURL, stored.Notifications.WebhookURL)
	keepPlaceholder(&next.SMB.Password, stored.SMB.Password)
	keepPlaceholder(&next.Strm.Secret, stored.Strm.Secret)

	rclone, storedRclone := &next.Mount.ExternalRclone, stored.Mount.ExternalRclone
	if raw, posted := lookup(patch, "mount", "external_rclone", "rc_password"); !posted || raw == secretPlaceholder {
		keepPlaceholder(&rclone.RCPassword, storedRclone.RCPassword)
		if rclone.RCPassword != "" && changedTo(rclone.RCUrl, storedRclone.RCUrl) {
			return fmt.Errorf("external rclone: rc_url changed, enter rc_password again")
		}
	}

	if entries, ok := patch["debrids"].([]any); ok {
		for i := range next.Debrids {
			d, entry := &next.Debrids[i], listEntry(entries, i)
			label := fmt.Sprintf("debrid %q", debridKey(*d))
			old := matchEntry(stored.Debrids, i, debridKey, debridKey(*d))
			var oldKey, oldRcPass *string
			var oldKeys []string
			var moved, rcMoved string
			if old != nil {
				oldKey, oldKeys, oldRcPass = &old.APIKey, old.DownloadAPIKeys, &old.RcPass
				if cmp.Or(d.Provider, d.Name) != cmp.Or(old.Provider, old.Name) ||
					changedTo(d.APIBaseURL(""), old.APIBaseURL("")) || changedTo(d.Proxy, old.Proxy) {
					moved = "provider, api_host or proxy"
				}
				if changedTo(d.RcUrl, old.RcUrl) {
					rcMoved = "rc_url"
				}
			}
			if err := keepEntrySecret(label, "api_key", entry, &d.APIKey, oldKey, moved); err != nil {
				return err
			}
			if err := keepDownloadKeys(label, entry, &d.DownloadAPIKeys, old != nil, oldKeys, moved); err != nil {
				return err
			}
			if err := keepEntrySecret(label, "rc_pass", entry, &d.RcPass, oldRcPass, rcMoved); err != nil {
				return err
			}
		}
	}

	if entries, ok := patch["arrs"].([]any); ok {
		for i := range next.Arrs {
			a := &next.Arrs[i]
			var oldToken *string
			var moved string
			if old := matchEntry(storedArrs, i, arrKey, a.Name); old != nil {
				oldToken = &old.Token
				if changedTo(strings.TrimRight(a.Host, "/"), strings.TrimRight(old.Host, "/")) {
					moved = "host"
				}
			}
			if err := keepEntrySecret(fmt.Sprintf("arr %q", a.Name), "token", listEntry(entries, i), &a.Token, oldToken, moved); err != nil {
				return err
			}
		}
	}

	if entries, ok := lookupList(patch, "usenet", "providers"); ok {
		for i := range next.Usenet.Providers {
			p := &next.Usenet.Providers[i]
			// The identity covers where the password goes, so a provider that
			// moved has no stored match and must send its password again.
			var oldPassword *string
			if old := matchEntry(stored.Usenet.Providers, i, config.UsenetProvider.ID, p.ID()); old != nil {
				oldPassword = &old.Password
			}
			if err := keepEntrySecret(fmt.Sprintf("usenet provider %s", p.ID()), "password", listEntry(entries, i), &p.Password, oldPassword, ""); err != nil {
				return err
			}
		}
	}

	return placeholderLeft(next)
}

// changedTo reports whether a secret's destination is now a different,
// non-empty value.
func changedTo(next, old string) bool {
	return next != "" && next != old
}

// placeholderLeft fails if any secret in c still holds the placeholder, so it
// is never stored in place of a credential.
func placeholderLeft(c *config.Config) error {
	var leftover error
	eachSecret(reflect.ValueOf(c).Elem(), func(typeName, field string, v reflect.Value) {
		secretStrings(v, func(s reflect.Value) {
			if leftover == nil && s.String() == secretPlaceholder {
				leftover = fmt.Errorf("%s.%s: %q has no stored value to stand for, enter it again", typeName, field, secretPlaceholder)
			}
		})
	})
	return leftover
}

func keepPlaceholder(value *string, stored string) {
	if *value == secretPlaceholder {
		*value = stored
	}
}

// keepEntrySecret resolves one secret of a posted list entry. stored is the
// matching stored entry's value, nil without a match; moved names what
// changed about the secret's destination, "" if nothing did.
func keepEntrySecret(label, field string, entry map[string]any, value *string, stored *string, moved string) error {
	raw, posted := entry[field]
	if posted && raw != secretPlaceholder {
		return nil // a new value, "" or null
	}
	switch {
	case stored == nil:
		if posted {
			return fmt.Errorf("%s: no stored %s to keep, enter it again", label, field)
		}
		return nil
	case *stored == "":
		*value = ""
		return nil
	case moved != "":
		return fmt.Errorf("%s: %s changed, enter %s again", label, moved, field)
	}
	*value = *stored
	return nil
}

// keepDownloadKeys resolves download_api_keys. Omitted keeps the stored list;
// a placeholder at index i keeps stored key i, since GET /api/config returns
// one placeholder per stored key in order.
func keepDownloadKeys(label string, entry map[string]any, value *[]string, matched bool, stored []string, moved string) error {
	if _, posted := entry["download_api_keys"]; !posted {
		if !matched || len(stored) == 0 {
			return nil
		}
		if moved != "" {
			return fmt.Errorf("%s: %s changed, enter download_api_keys again", label, moved)
		}
		*value = slices.Clone(stored)
		return nil
	}
	for i, key := range *value {
		if key != secretPlaceholder {
			continue
		}
		if !matched || i >= len(stored) {
			return fmt.Errorf("%s: no stored download_api_keys[%d] to keep, enter it again", label, i)
		}
		if moved != "" {
			return fmt.Errorf("%s: %s changed, enter download_api_keys again", label, moved)
		}
		(*value)[i] = stored[i]
	}
	return nil
}

// matchEntry returns the stored entry whose key is want, preferring the one
// at index i when several share it.
func matchEntry[T any](stored []T, i int, key func(T) string, want string) *T {
	if i < len(stored) && key(stored[i]) == want {
		return &stored[i]
	}
	if j := slices.IndexFunc(stored, func(s T) bool { return key(s) == want }); j >= 0 {
		return &stored[j]
	}
	return nil
}

func debridKey(d config.Debrid) string { return cmp.Or(d.Name, d.Provider) }

func arrKey(a config.Arr) string { return a.Name }

func listEntry(entries []any, i int) map[string]any {
	if i >= len(entries) {
		return nil
	}
	entry, _ := entries[i].(map[string]any)
	return entry
}

// lookup returns the value at path in a decoded JSON object, and whether every
// key on the way was present.
func lookup(object map[string]any, path ...string) (any, bool) {
	var current any = object
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = m[key]; !ok {
			return nil, false
		}
	}
	return current, true
}

func lookupList(object map[string]any, path ...string) ([]any, bool) {
	value, _ := lookup(object, path...)
	list, ok := value.([]any)
	return list, ok
}
