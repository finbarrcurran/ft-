package videoitems

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Word is one transcript word with the time of the 15-second marker block it sits in.
type Word struct {
	T int
	W string
}

var markerLine = regexp.MustCompile(`^\[(\d+)s\]\s?(.*)$`)

// WordsFromCompact turns the compact transcript the model reads ("[NNNs] text" lines) into words.
// Using the same text the model saw keeps the gate consistent with what it was asked to cite.
func WordsFromCompact(compact string) []Word {
	var out []Word
	cur := 0
	for _, line := range strings.Split(compact, "\n") {
		text := line
		if m := markerLine.FindStringSubmatch(line); m != nil {
			cur, _ = strconv.Atoi(m[1])
			text = m[2]
		}
		for _, w := range strings.Fields(text) {
			out = append(out, Word{T: cur, W: w})
		}
	}
	return out
}

// Window returns the passage the gate checks an item against: from 15 s before the cited start to
// 45 s after the cited end (a cited span longer than 45 s is clamped), at most 200 words.
func Window(words []Word, start, end int) string {
	if end-start > 45 {
		end = start + 45
	}
	var b []string
	for _, w := range words {
		if w.T >= start-15 && w.T < end+45 {
			b = append(b, w.W)
			if len(b) == 200 {
				break
			}
		}
	}
	return strings.Join(b, " ")
}

// ---------------------------------------------------------------------------
// Numbers

var numberWords = map[string]int{"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8,
	"nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17,
	"eighteen": 18, "nineteen": 19, "twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90}

var scaleWords = map[string]float64{"thousand": 1e3, "million": 1e6, "billion": 1e9, "trillion": 1e12, "k": 1e3, "m": 1e6, "b": 1e9, "t": 1e12}

// splitSep splits s into words and separators (a run of whitespace, or one hyphen), keeping both.
func splitSep(s string) []string {
	var toks []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '-':
			toks = append(toks, "-")
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		default:
			j := i
			for j < len(s) && s[j] != '-' && s[j] != ' ' && s[j] != '\t' && s[j] != '\n' && s[j] != '\r' {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks
}

func isSep(t string) bool { return t == "-" || strings.TrimSpace(t) == "" }

// wordsToDigits replaces number words ("twenty five", "three", "one hundred") with digits.
func wordsToDigits(text string) string {
	toks := splitSep(strings.ToLower(text))
	var out strings.Builder
	for i := 0; i < len(toks); {
		v, ok := numberWords[toks[i]]
		if !ok {
			out.WriteString(toks[i])
			i++
			continue
		}
		j := i + 1
		k := j
		for k < len(toks) && isSep(toks[k]) {
			k++
		}
		if v >= 20 && k < len(toks) {
			if o, ok2 := numberWords[toks[k]]; ok2 && o < 10 {
				v += o
				j = k + 1
			}
		}
		k = j
		for k < len(toks) && isSep(toks[k]) {
			k++
		}
		if k < len(toks) && toks[k] == "hundred" {
			v *= 100
			j = k + 1
		}
		out.WriteString(strconv.Itoa(v))
		i = j
	}
	return out.String()
}

// stripThousands removes the commas in 1,234,567.
func stripThousands(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == ',' && i > 0 && s[i-1] >= '0' && s[i-1] <= '9' && i+3 < len(s) && allDigits(s[i+1:i+4]) &&
			(i+4 >= len(s) || s[i+4] < '0' || s[i+4] > '9') {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var numSuffixRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(%|percent|thousand|million|billion|trillion|[kmbt])?`)

// NumValues returns every numeric value in text: plain figures, scaled ones ("20 trillion", "100K") and
// number words, with thousands separators removed.
func NumValues(text string) []float64 {
	t := stripThousands(wordsToDigits(text))
	var vals []float64
	for _, m := range numSuffixRe.FindAllStringSubmatchIndex(t, -1) {
		v, err := strconv.ParseFloat(t[m[2]:m[3]], 64)
		if err != nil {
			continue
		}
		vals = append(vals, v)
		if m[4] >= 0 {
			suf := t[m[4]:m[5]]
			if len(suf) == 1 && m[5] < len(t) && isLetter(t[m[5]]) {
				continue // "5 men", "20 buy": a letter, not a scale suffix
			}
			if sc, ok := scaleWords[suf]; ok {
				vals = append(vals, v*sc)
			}
		}
	}
	return vals
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func hasValue(vals []float64, v float64) bool {
	for _, w := range vals {
		if w == v || math.Abs(v-w) <= 1e-9*math.Max(1, math.Abs(w)) {
			return true
		}
	}
	return false
}

var plainNumRe = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)

// ItemNumbers returns the figures written in an item's text, with the left-boundary rule (not part of
// a word or a decimal) and trailing punctuation removed.
func ItemNumbers(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, loc := range plainNumRe.FindAllStringIndex(text, -1) {
		if loc[0] > 0 {
			p := text[loc[0]-1]
			if isLetter(p) || (p >= '0' && p <= '9') || p == '.' {
				continue
			}
		}
		n := strings.TrimRight(text[loc[0]:loc[1]], ",.")
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
