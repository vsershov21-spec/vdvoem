package app

import "testing"

func TestPageTitleAndLatinNames(t *testing.T) {
	if !PageTitle("Афиша мероприятий Московского Планетария, цены на официальном сайте") || PageTitle("«Эпоха звёзд» в Планетарии") || PageTitle("Каток на ВДНХ") {
		t.Fatal("PageTitle")
	}
	if Past("Anastasiya", "отметил") != "отметила" || Past("Nikita", "отметил") != "отметил" || Past("Vladimir", "отметил") != "отметил" {
		t.Fatal("Past latin")
	}
}
