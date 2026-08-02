package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"blood-pressure-bot/lib/timeloc"
	"blood-pressure-bot/storage"
)

// dayParts — фиксированный порядок частей суток в строке CSV. Локальная
// константа: в events/telegram они не экспортируются, зависимости на слой бота
// не тащим.
var dayParts = []string{"утро", "день", "вечер"}

// cliExportHeader — шапка CSV-выгрузки CLI: две колонки идентификации
// пользователя.
const cliExportHeader = "User_ID;User_name;Дата;Утро;День;Вечер"

// defaultExportName возвращает имя файла выгрузки по умолчанию.
func defaultExportName() string {
	return "export_" + timeloc.Now().Format(timeloc.DateFormat) + ".csv"
}

// runExport выполняет подкоманду export.
func runExport(args []string, s store) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ff := addFilterFlags(fs)
	out := fs.String("out", defaultExportName(), "путь к файлу выгрузки")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("не удалось разобрать флаги export: %w", err)
	}

	filter, err := ff.filter()
	if err != nil {
		return err
	}

	pressures, err := s.ExportAll(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("не удалось получить показания: %w", err)
	}
	if len(pressures) == 0 {
		fmt.Println("Нет данных по заданным фильтрам — файл не создан")
		return nil
	}

	if err := os.WriteFile(*out, []byte(formatExportCSV(pressures)), 0o644); err != nil {
		return fmt.Errorf("не удалось записать файл %s: %w", *out, err)
	}

	fmt.Printf("Выгружено %d записей в %s\n", len(pressures), *out)

	return nil
}

// formatExportCSV собирает CSV-выгрузку CLI: одна строка на (пользователь,
// дата), отсутствующие части суток — пустые ячейки. BOM и CRLF нужны для
// корректного открытия в Excel. User_ID для legacy-строк (0) — пустая ячейка.
func formatExportCSV(pressures []storage.Pressure) string {
	// Ключ строки — (user_id, user_name, date): у одного пользователя может
	// быть несколько строк с разными user_name, их разделяем.
	type rowKey struct {
		userID   int64
		userName string
		date     string
	}

	byRow := make(map[rowKey]map[string]storage.Pressure)
	var keys []rowKey
	for _, p := range pressures {
		key := rowKey{userID: p.UserID, userName: p.UserName, date: p.Date}
		if byRow[key] == nil {
			byRow[key] = make(map[string]storage.Pressure)
			keys = append(keys, key)
		}
		byRow[key][p.DayPart] = p
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].userID != keys[j].userID {
			return keys[i].userID < keys[j].userID
		}
		if keys[i].userName != keys[j].userName {
			return keys[i].userName < keys[j].userName
		}
		return keys[i].date < keys[j].date
	})

	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF")
	b.WriteString(cliExportHeader)
	b.WriteString("\r\n")

	for _, key := range keys {
		fields := []string{formatUserID(key.userID), key.userName, formatCSVDate(key.date)}
		for _, part := range dayParts {
			if p, ok := byRow[key][part]; ok {
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

// formatUserID возвращает текстовое представление user_id; 0 (legacy-строка)
// — пустая ячейка.
func formatUserID(id int64) string {
	if id == 0 {
		return ""
	}

	return strconv.FormatInt(id, 10)
}
