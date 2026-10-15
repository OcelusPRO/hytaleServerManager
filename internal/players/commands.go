// Package players handles player management: moderation commands, the
// server's access lists (whitelist, bans, operators) and online presence.
package players

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// Action is a moderation action sent to the server console.
type Action string

// Supported actions.
const (
	ActionKick            Action = "kick"
	ActionBan             Action = "ban"
	ActionUnban           Action = "unban"
	ActionOp              Action = "op"
	ActionDeop            Action = "deop"
	ActionWhitelistAdd    Action = "whitelist-add"
	ActionWhitelistRemove Action = "whitelist-remove"
)

// Command errors.
var (
	ErrUnknownAction = errors.New("action inconnue")
	ErrInvalidPlayer = errors.New("invalid player name")
	ErrInvalidReason = errors.New("invalid reason")
)

var playerPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

// Console command templates (Hytale 0.7); %p is a player name or UUID.
var commandTemplates = map[Action]string{
	ActionKick:            "/kick %p",
	ActionBan:             "/ban %p",
	ActionUnban:           "/unban %p",
	ActionOp:              "/op add %p",
	ActionDeop:            "/op remove %p",
	ActionWhitelistAdd:    "/whitelist add %p",
	ActionWhitelistRemove: "/whitelist remove %p",
}

// Only /ban accepts trailing text, used as the ban reason.
var actionsWithReason = map[Action]bool{ActionBan: true}

// BuildCommand returns the console command for an action. Inputs are
// strictly validated so that nothing can be smuggled into the console.
func BuildCommand(action Action, player, reason string) (string, error) {
	tmpl, ok := commandTemplates[action]
	if !ok {
		return "", ErrUnknownAction
	}
	if !playerPattern.MatchString(player) && !ValidUUID(player) {
		return "", ErrInvalidPlayer
	}
	cmd := strings.Replace(tmpl, "%p", player, 1)

	reason = strings.TrimSpace(reason)
	if reason == "" || !actionsWithReason[action] {
		return cmd, nil
	}
	if len(reason) > 200 || strings.ContainsFunc(reason, unicode.IsControl) {
		return "", ErrInvalidReason
	}
	return cmd + " " + reason, nil
}

// WhitelistCommand enables or disables the whitelist.
func WhitelistCommand(enabled bool) string {
	if enabled {
		return "/whitelist on"
	}
	return "/whitelist off"
}
