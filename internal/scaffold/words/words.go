package words

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var Adjectives = []string{
	"active", "agile", "alert", "apt", "bright", "brisk", "calm", "capable", "clear",
	"clever", "confident", "cool", "crisp", "devoted", "diligent", "distinct", "dynamic",
	"eager", "effective", "efficient", "energetic", "exact", "fair", "fast", "firm", "flexible",
	"focused", "fresh", "global", "grand", "handy", "happy", "helpful", "ideal", "keen", "lively",
	"loyal", "master", "modern", "neat", "optimal", "original", "patient", "peak", "perfect",
	"planned", "polite", "potent", "precise", "prime", "prompt", "proud", "pure", "quick", "quiet",
	"rapid", "ready", "reliable", "robust", "secure", "sharp", "simple", "smart", "solid", "sound",
	"spare", "stable", "steady", "strong", "superb", "swift", "tactical", "technical", "tidy",
	"top", "true", "useful", "valid", "vital", "vivid", "warm", "wise", "whole", "willing",
}

var Nouns = []string{
	"agent", "anchor", "beacon", "bridge", "builder", "catalyst", "center", "cloud", "core",
	"data", "device", "driver", "element", "engine", "explorer", "field", "flow", "forge", "frame",
	"gateway", "grid", "guard", "handler", "helper", "hub", "interface", "kernel", "layer",
	"link", "manager", "mapper", "monitor", "network", "node", "observer", "operator", "panel",
	"parser", "pilot", "pointer", "portal", "processor", "provider", "reactor", "recorder",
	"reflector", "resolver", "router", "runner", "scanner", "scheduler", "sensor", "server",
	"signal", "source", "stream", "system", "tracker", "validator", "viewer", "worker",
}

const (
	MaxDbNameLength = 63
	SuffixMaxLength = 25
)

func GenerateSuffix() string {
	bytes := make([]byte, 4)
	if _, err := cryptorand.Read(bytes); err != nil {
		return fmt.Sprintf("%d_%d", time.Now().UnixNano()%100000, os.Getpid()%1000)
	}

	adjIndex := int(binary.LittleEndian.Uint16(bytes[0:2])) % len(Adjectives)
	nounIndex := int(binary.LittleEndian.Uint16(bytes[2:4])) % len(Nouns)

	return fmt.Sprintf("%s_%s", Adjectives[adjIndex], Nouns[nounIndex])
}

func SanitizeSiteName(name string) string {
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9_]`)
	name = re.ReplaceAllString(name, "_")
	re = regexp.MustCompile(`_+`)
	name = re.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	return name
}

func GenerateDatabaseName(siteName string, maxLength int) string {
	return BuildDatabaseName(siteName, GenerateSuffix(), maxLength)
}

func BuildDatabaseName(siteName string, suffix string, maxLength int) string {
	if maxLength == 0 {
		maxLength = MaxDbNameLength
	}

	sanitized := SanitizeSiteName(siteName)

	maxSiteLen := maxLength - len(suffix) - 1
	if maxSiteLen < 1 {
		maxSiteLen = 1
	}
	if len(sanitized) > maxSiteLen {
		sanitized = sanitized[:maxSiteLen]
		sanitized = strings.TrimRight(sanitized, "_")
	}

	return fmt.Sprintf("%s_%s", sanitized, suffix)
}

func ExtractSuffix(dbName string) string {
	parts := strings.Split(dbName, "_")
	if len(parts) < 2 {
		return ""
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]

	potentialSuffix := fmt.Sprintf("%s_%s", secondLastPart, lastPart)

	for _, noun := range Nouns {
		if noun == lastPart {
			for _, adj := range Adjectives {
				if adj == secondLastPart {
					return potentialSuffix
				}
			}
		}
	}

	return ""
}
