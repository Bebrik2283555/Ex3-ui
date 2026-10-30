package sidecar

// Seed dictionaries the olcRTC core uses to invent display names. They are
// written once into a fresh data directory and never replaced.

import "strings"

var olcrtcNames = strings.Join([]string{
	"Alexander", "Alexey", "Anatoly", "Andrey", "Anton", "Artyom",
	"Boris", "Denis", "Dmitry", "Egor", "Evgeny", "Ivan", "Igor", "Ilya",
	"Kirill", "Leonid", "Maxim", "Mikhail", "Nikita", "Nikolay", "Oleg",
	"Pavel", "Roman", "Sergei", "Stepan", "Timofey", "Viktor", "Vladimir",
	"Anna", "Anastasia", "Daria", "Ekaterina", "Elena", "Irina", "Ksenia",
	"Maria", "Natalia", "Olga", "Polina", "Svetlana", "Tatiana", "Vera",
	"Victoria", "Yulia",
}, "\n")

var olcrtcSurnames = strings.Join([]string{
	"Antonov", "Belov", "Volkov", "Gavrilov", "Danilov", "Egorov", "Efimov",
	"Zhukov", "Zaitsev", "Ivanov", "Ilyin", "Kozlov", "Kuznetsov", "Lebedev",
	"Makarov", "Melnikov", "Morozov", "Nikolaev", "Novikov", "Orlov", "Pavlov",
	"Petrov", "Popov", "Romanov", "Savin", "Smirnov", "Sokolov", "Stepanov",
	"Tarasov", "Titov", "Fedorov", "Filatov", "Gusev", "Tsvetkov", "Chernov",
	"Shevtsov", "Shcherbakov", "Yuriev", "Yakovlev",
}, "\n")
