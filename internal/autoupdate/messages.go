package autoupdate

import (
	"fmt"
	"time"
)

// playerTexts are the announcements shown to players in game. They cannot
// follow each player's language: the operator picks one in the settings.
type playerTexts struct {
	stoppingTitle, stoppingSubtitle string
	updateIn, restarting            string
	minute, minutes, seconds        string
}

var texts = map[string]playerTexts{
	"en": {
		stoppingTitle:    "Server stopping",
		stoppingSubtitle: "Updating, it will be back in a moment",
		updateIn:         "Server update in %s",
		restarting:       "The server will restart",
		minute:           "1 minute",
		minutes:          "%d minutes",
		seconds:          "%d seconds",
	},
	"fr": {
		stoppingTitle:    "Arrêt du serveur",
		stoppingSubtitle: "Mise à jour en cours, il redémarre dans un instant",
		updateIn:         "Mise à jour du serveur dans %s",
		restarting:       "Le serveur va redémarrer",
		minute:           "1 minute",
		minutes:          "%d minutes",
		seconds:          "%d secondes",
	},
}

func textsFor(lang string) playerTexts {
	if t, ok := texts[lang]; ok {
		return t
	}
	return texts["en"]
}

// describe spells a countdown step out.
func (t playerTexts) describe(d time.Duration) string {
	switch {
	case d == time.Minute:
		return t.minute
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf(t.minutes, int(d/time.Minute))
	default:
		return fmt.Sprintf(t.seconds, int(d/time.Second))
	}
}
