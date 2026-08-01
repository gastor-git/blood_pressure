package telegram

import (
	"sort"
	"strings"
	"time"

	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// csvOrder — фиксированный порядок частей суток в строке CSV.
var csvOrder = []string{dayPartMorning, dayPartDay, dayPartEvening}

// formatCSV собирает CSV выгрузки: одна строка на дату, отсутствующие части
// суток — пустые ячейки. BOM и CRLF нужны для корректного открытия в Excel.
func formatCSV(pressures []storage.Pressure) string {
	byDate := make(map[string]map[string]storage.Pressure)
	for _, p := range pressures {
		if byDate[p.Date] == nil {
			byDate[p.Date] = make(map[string]storage.Pressure)
		}
		byDate[p.Date][p.DayPart] = p
	}

	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF")
	b.WriteString(msgCSVHeader)
	b.WriteString("\r\n")

	for _, date := range dates {
		fields := []string{formatCSVDate(date)}
		for _, part := range csvOrder {
			if p, ok := byDate[date][part]; ok {
				fields = append(fields, p.Systolic+"/"+p.Diastolic+"/"+p.HeartRate)
			} else {
				fields = append(fields, "")
			}
		}
		b.WriteString(strings.Join(fields, ";"))
		b.WriteString("\r\n")
	}

	return b.String()
}

// formatCSVDate приводит дату из формата хранения к виду DD.MM.YYYY.
func formatCSVDate(date string) string {
	t, err := time.Parse(timeloc.DateFormat, date)
	if err != nil {
		return date
	}

	return t.Format(timeloc.CSVDateFormat)
}

// csvFilename возвращает имя файла выгрузки; при пустом username — "user".
func csvFilename(username string) string {
	if username == "" {
		username = "user"
	}

	return username + "_" + timeloc.Now().Format(timeloc.DateFormat) + ".csv"
}
