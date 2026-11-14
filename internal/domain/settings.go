package domain

// Settings are one screen with three groups: mine, the couple's, data. Every setting has an owner
// (me — only I see and change it; pair — either changes it and the other is told), a default, and a closed
// set of values. Unknown keys and values are refused, so the screen and the server cannot drift apart.

type SettingDef struct {
	Key     string   `json:"key"`
	Scope   string   `json:"scope"` // me | pair
	Kind    string   `json:"kind"`  // bool | choice | text
	Default string   `json:"default"`
	Values  []string `json:"values,omitempty"`
	Label   string   `json:"label"` // how the bot names it when the partner changes a shared one
}

const (
	ScopeMe   = "me"
	ScopePair = "pair"
)

var Settings = []SettingDef{
	// notifications: what is repeated in the bot (the home screen always shows everything)
	{Key: "notify.proposals", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Предложения партнёра"},
	{Key: "notify.meeting", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Напоминание о встрече"},
	{Key: "notify.meeting_when", Scope: ScopeMe, Kind: "choice", Default: "2h", Values: []string{"2h", "day", "both"}, Label: "Когда напоминать о встрече"},
	{Key: "notify.chores", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Дела на сегодня и просроченные"},
	{Key: "notify.scout", Scope: ScopeMe, Kind: "bool", Default: "0", Label: "Находки скаута"},
	{Key: "notify.after", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Как прошла встреча"},
	{Key: "notify.quiet", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Тихие часы"},
	// calendar
	{Key: "cal.meetings", Scope: ScopeMe, Kind: "bool", Default: "1", Label: "Встречи в календарь телефона"},
	{Key: "cal.busy", Scope: ScopeMe, Kind: "choice", Default: "manual", Values: []string{"manual", "calendar"}, Label: "Моя занятость"},
	// the couple's
	{Key: "meet.budget", Scope: ScopePair, Kind: "choice", Default: "5000", Values: []string{"3000", "5000", "none"}, Label: "Бюджет на встречу"},
	{Key: "meet.walk", Scope: ScopePair, Kind: "choice", Default: "15", Values: []string{"15", "30", "transit"}, Label: "Между местами"},
	{Key: "scout.kino", Scope: ScopePair, Kind: "bool", Default: "1", Label: "Скаут ищет кино"},
	{Key: "scout.concert", Scope: ScopePair, Kind: "bool", Default: "1", Label: "Скаут ищет концерты"},
	{Key: "scout.theatre", Scope: ScopePair, Kind: "bool", Default: "0", Label: "Скаут ищет театр"},
	{Key: "scout.food", Scope: ScopePair, Kind: "bool", Default: "1", Label: "Скаут ищет еду"},
	{Key: "scout.active", Scope: ScopePair, Kind: "bool", Default: "0", Label: "Скаут ищет активное"},
	{Key: "scout.freq", Scope: ScopePair, Kind: "choice", Default: "daily", Values: []string{"daily", "weekdays", "weekly"}, Label: "Как часто ищет скаут"},
	{Key: "scout.avoid", Scope: ScopePair, Kind: "text", Default: "", Label: "Не предлагать"},
	{Key: "chores.remind", Scope: ScopePair, Kind: "choice", Default: "9", Values: []string{"9", "19", "off"}, Label: "Утреннее напоминание о делах"},
	{Key: "chores.keep", Scope: ScopePair, Kind: "choice", Default: "7", Values: []string{"7", "30"}, Label: "Сколько держать сделанные дела"},
	{Key: "chores.overdue_home", Scope: ScopePair, Kind: "bool", Default: "1", Label: "Просроченные дела на главной"},
}

func SettingOf(key string) (SettingDef, bool) {
	for _, d := range Settings {
		if d.Key == key {
			return d, true
		}
	}
	return SettingDef{}, false
}

// ValidSetting: the key exists and the value is one it can take
func ValidSetting(key, value string) bool {
	d, ok := SettingOf(key)
	if !ok {
		return false
	}
	switch d.Kind {
	case "bool":
		return value == "0" || value == "1"
	case "choice":
		for _, v := range d.Values {
			if v == value {
				return true
			}
		}
		return false
	}
	return len([]rune(value)) <= 200
}

// ResolveSettings: the stored values over the defaults, for one scope
func ResolveSettings(scope string, stored map[string]string) map[string]string {
	out := map[string]string{}
	for _, d := range Settings {
		if d.Scope != scope {
			continue
		}
		out[d.Key] = d.Default
		if v, ok := stored[d.Key]; ok && ValidSetting(d.Key, v) {
			out[d.Key] = v
		}
	}
	return out
}

var settingWords = map[string]string{
	"2h": "за 2 часа", "day": "за день", "both": "за день и за 2 часа", "manual": "отмечаю сам", "calendar": "из календаря",
	"3000": "до 3 000 ₽", "5000": "до 5 000 ₽", "none": "без лимита", "15": "пешком до 15 минут", "30": "пешком до 30 минут", "transit": "на транспорте",
	"daily": "каждый день", "weekdays": "по будням", "weekly": "раз в неделю", "9": "в 9:00", "19": "в 19:00", "off": "не напоминать", "7": "7 дней",
}

// SettingWords: the value as a person reads it («до 3 000 ₽», «включено»)
func SettingWords(key, value string) string {
	d, _ := SettingOf(key)
	switch {
	case d.Kind == "bool" && value == "1":
		return "включено"
	case d.Kind == "bool":
		return "выключено"
	case key == "chores.keep":
		return value + " дней"
	case d.Kind == "text" && value == "":
		return "пусто"
	case d.Kind == "text":
		return value
	}
	if w, ok := settingWords[value]; ok {
		return w
	}
	return value
}
