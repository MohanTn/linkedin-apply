package service

import (
	"regexp"
	"sort"
	"strings"
)

// minJDChars: below this the JD carries too little signal to score meaningfully.
const minJDChars = 200

// maxKeywords caps how many distinct JD keywords are scored/reported.
const maxKeywords = 40

// AtsResult is the resume-vs-JD match outcome.
type AtsResult struct {
	Score   int      `json:"score"`   // 0-100 weighted keyword coverage
	Matched []string `json:"matched"` // JD keywords found in the resume
	Missing []string `json:"missing"` // JD keywords absent from the resume
}

type weightedKeyword struct {
	word   string
	weight int // frequency in the JD
}

var wordRe = regexp.MustCompile(`[A-Za-zÀ-ÿ][A-Za-zÀ-ÿ0-9+#.\-]{1,}`)

// ScoreResume computes a 0-100 keyword-coverage match of the resume against the
// job description. A JD shorter than minJDChars yields a zero result flagged by
// an empty keyword set, so callers can skip storing a misleading number.
func ScoreResume(resumeText, jdText string) AtsResult {
	if len(strings.TrimSpace(jdText)) < minJDChars {
		return AtsResult{}
	}
	keywords := extractKeywords(jdText)
	if len(keywords) == 0 {
		return AtsResult{}
	}

	resumeTokens := tokenSet(resumeText)
	var matched, missing []string
	var totalWeight, hitWeight int
	for _, kw := range keywords {
		totalWeight += kw.weight
		if resumeTokens[kw.word] {
			hitWeight += kw.weight
			matched = append(matched, kw.word)
		} else {
			missing = append(missing, kw.word)
		}
	}

	score := 0
	if totalWeight > 0 {
		score = int(float64(hitWeight) / float64(totalWeight) * 100)
	}
	return AtsResult{Score: score, Matched: matched, Missing: missing}
}

// extractKeywords returns the JD's most frequent meaningful tokens (stopwords
// and very short tokens removed), heaviest first, capped at maxKeywords.
func extractKeywords(jd string) []weightedKeyword {
	freq := map[string]int{}
	for _, tok := range wordRe.FindAllString(strings.ToLower(jd), -1) {
		tok = strings.Trim(tok, ".-")
		if len(tok) < 3 || stopwords[tok] {
			continue
		}
		freq[tok]++
	}
	out := make([]weightedKeyword, 0, len(freq))
	for w, n := range freq {
		out = append(out, weightedKeyword{word: w, weight: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].weight != out[j].weight {
			return out[i].weight > out[j].weight
		}
		return out[i].word < out[j].word
	})
	if len(out) > maxKeywords {
		out = out[:maxKeywords]
	}
	return out
}

func tokenSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range wordRe.FindAllString(strings.ToLower(text), -1) {
		set[strings.Trim(tok, ".-")] = true
	}
	return set
}

// stopwords covers common German and English filler words so keyword coverage
// reflects skills/requirements, not grammar. Language-neutral skill tokens
// (go, kubernetes, sap, react) are intentionally never stopwords.
var stopwords = func() map[string]bool {
	list := []string{
		// English
		"the", "and", "for", "you", "your", "our", "with", "will", "are", "have",
		"has", "was", "were", "this", "that", "they", "them", "from", "not", "but",
		"all", "any", "can", "may", "who", "what", "when", "where", "which", "how",
		"job", "role", "work", "team", "years", "year", "experience", "skills",
		"ability", "strong", "good", "well", "must", "should", "would", "about",
		"more", "than", "into", "out", "per", "via", "etc", "such", "also", "each",
		"within", "across", "using", "use", "used", "including", "include", "based",
		"new", "one", "two", "three", "we", "us", "as", "at", "in", "on", "of", "to",
		"or", "an", "is", "be", "by", "it", "if", "so",
		// Job-board UI / page chrome that leaks in even after container extraction
		"apply", "applicant", "applicants", "skip", "save", "saved", "share",
		"report", "sign", "join", "login", "logout", "premium", "reactivate",
		"jobs", "job", "search", "home", "profile", "settings", "notifications",
		"messaging", "network", "feed", "linkedin", "glassdoor", "xing", "indeed",
		"stepstone", "company", "companies", "see", "get", "show", "more", "less",
		"view", "click", "here", "back", "next", "previous", "menu", "close",
		"cookie", "cookies", "privacy", "terms", "policy", "pricing", "members",
		"member", "exclusive", "access", "offer", "off", "chart", "reviews",
		"review", "salary", "salaries", "follow", "following", "connect", "message",
		"posted", "ago", "today", "yesterday", "date", "location", "remote",
		"fulltime", "parttime", "contract", "internship", "level", "senior", "junior",
		"mid", "entry", "hybrid", "onsite",
		// Month tokens (job dates leak in)
		"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov",
		"dec", "januar", "februar", "märz", "april", "juni", "juli", "august",
		"september", "oktober", "november", "dezember",
		// German UI chrome
		"bewerben", "speichern", "teilen", "anmelden", "registrieren", "merken",
		"stelle", "stellen", "unternehmen", "gehalt", "standort", "heute", "gestern",
		// German
		"und", "oder", "der", "die", "das", "den", "dem", "des", "ein", "eine",
		"einen", "einer", "eines", "für", "mit", "von", "aus", "auf", "bei", "als",
		"auch", "sind", "wird", "werden", "wurde", "haben", "hat", "sich", "nicht",
		"sie", "wir", "uns", "unser", "ihre", "ihr", "sein", "seine", "über", "unter",
		"zum", "zur", "sowie", "sowohl", "dass", "damit", "durch", "nach", "vor",
		"bis", "kann", "können", "soll", "sollte", "muss", "wenn", "dann", "diese",
		"dieser", "dieses", "jahre", "jahren", "erfahrung", "kenntnisse", "aufgaben",
		"gute", "guten", "im", "ist", "am", "zu", "in", "an",
	}
	m := make(map[string]bool, len(list))
	for _, w := range list {
		m[w] = true
	}
	return m
}()
