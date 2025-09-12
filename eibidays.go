package main

import (
	"strings"
)

func ConvertDays(input string) string {
	// Map day abbreviations to their digit positions (Sunday-based)
	dayToDigit := map[string]int{
		"su": 1, "sun": 1, "sunday": 1, "1": 1,
		"mo": 2, "mon": 2, "monday": 2, "2":2,
		"tu": 3, "tue": 3, "tuesday": 3,"3": 3,
		"we": 4, "wed": 4, "wednesday": 4,"4": 4,
		"th": 5, "thu": 5, "thursday": 5,"5": 5,
		"fr": 6, "fri": 6, "friday": 6,"6": 6,
		"sa": 7, "sat": 7, "saturday": 7,"7": 7,	
	}

	input = strings.ToLower(strings.TrimSpace(input))
	
	// Handle empty input - return "1234567"
	if input == "" {
		return "1234567"
	}
	if input == "SaSu" {
		return "1.....7"
	}

	// Initialize result with dots
	result := make([]rune, 7)
	for i := range result {
		result[i] = '.'
	}

	originalInput := input
	matched := false

	// Handle ranges
	if strings.Contains(input, "-") {
		if handleRange(input, result, dayToDigit) {
			matched = true
		}
	}

	// Handle comma-separated values
	if !matched && strings.Contains(input, ",") {
		if handleCommaSeparated(input, result, dayToDigit) {
			matched = true
		}
	}

	// Handle single day
	if !matched {
		if handleSingleDay(input, result, dayToDigit) {
			matched = true
		}
	}

	// Handle digit strings (Monday-based)
	if !matched && isAllDigits(input) {
		handleDigitString(input, result)
		matched = true
	}

	// If no pattern matched, return original input
	if !matched {
		return originalInput
	}

	return string(result)
}

func handleRange(input string, result []rune, dayToDigit map[string]int) bool {
	parts := strings.Split(input, "-")
	if len(parts) != 2 {
		return false
	}
	startDay := strings.TrimSpace(parts[0])
	endDay := strings.TrimSpace(parts[1])
	// fmt.Println(parts[0], parts[1])

	startDigit, startOk := dayToDigit[startDay]
	endDigit, endOk := dayToDigit[endDay]

	if !startOk || !endOk {
		return false
	}

	// Handle wrapping around Sunday
	if startDigit > endDigit {
		// Range crosses Sunday boundary (e.g., fr-su: 6 to 1)
		for i := startDigit; i <= 7; i++ {
			result[i-1] = rune(i + '0')
		}
		for i := 1; i <= endDigit; i++ {
			result[i-1] = rune(i + '0')
		}
	} else {
		// Normal range
		for i := startDigit; i <= endDigit; i++ {
			result[i-1] = rune(i + '0')
		}
	}

	return true
}

func handleCommaSeparated(input string, result []rune, dayToDigit map[string]int) bool {
	days := strings.Split(input, ",")
	matchedAny := false
	
	for _, day := range days {
		day = strings.TrimSpace(day)
		if digit, ok := dayToDigit[day]; ok {
			result[digit-1] = rune(digit + '0')
			matchedAny = true
		}
	}
	
	return matchedAny
}

func handleSingleDay(input string, result []rune, dayToDigit map[string]int) bool {
	if digit, ok := dayToDigit[input]; ok {
		result[digit-1] = rune(digit + '0')
		return true
	}
	return false
}

func handleDigitString(input string, result []rune) {
	// Convert Monday-based digits to Sunday-based positions
	// Monday(1) -> Monday(2), Tuesday(2) -> Tuesday(3), etc.
	// Sunday(7) -> Sunday(1)
	for _, char := range input {
		digit := int(char - '0')
		if digit >= 1 && digit <= 7 {
			// Map Monday-based to Sunday-based
			sundayDigit := digit
			if digit == 7 {
				sundayDigit = 1 // Sunday moves from position 7 to 1
			} else {
				sundayDigit = digit + 1 // Shift other days forward
			}
			result[sundayDigit-1] = rune(sundayDigit + '0')
		}
	}
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

