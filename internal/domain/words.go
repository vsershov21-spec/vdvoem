package domain

import "strings"

// The new interface has one word for one thing (docs/v2/PROJECT.md, «Словарь»): what the couple collects are
// «идеи». The old screens call the same list «копилка», and many messages were written in their words.
// NewWords puts such a message into the words of the new interface.
var newWords = strings.NewReplacer(
	"в общую копилку", "в общие идеи",
	"из вашей копилки", "из ваших идей", "из нашей копилки", "из наших идей",
	"вашей копилки", "ваших идей", "нашей копилки", "наших идей",
	"Сохранил в копилку", "Сохранил в идеи",
	"вернулась в копилку", "вернулась в идеи",
	"Новое в копилке", "Новое в идеях",
	"в афише и копилке", "в афише и идеях", "В афише и копилке", "В афише и идеях",
	"ни в копилке", "ни в идеях",
	"из копилки", "из идей", "Из копилки", "Из идей",
	"в копилку", "в идеи", "В копилку", "В идеи",
	"в копилке", "в идеях", "В копилке", "В идеях",
	"копилку", "идеи", "копилке", "идеях", "копилки", "идей", "копилкой", "идеями",
	"Копилка", "Идеи", "копилка", "идеи",
)

func NewWords(s string) string {
	if !strings.Contains(s, "опилк") {
		return s
	}
	return newWords.Replace(s)
}
