package enumword

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// wordOverrides holds the enum values whose word is not derived from their
// name, keyed by value name.
var wordOverrides = map[protoreflect.Name]string{
	"PHASE_UNSPECIFIED":       "idle",
	"TARGET_KIND_ONE_SHOT":    "1shot",
	"TARGET_KIND_LONG_RUNNER": "longr",
}

// Word is the word the CLI and the UI print for a proto enum value, and the
// one a flag takes: the value's name past its enum's prefix, lowercased, with
// "-" for "_". MODE_GO_EXEC reads "go-exec". An unspecified value reads "-", a
// value this binary does not know reads "?", and wordOverrides holds the
// exceptions.
func Word(e protoreflect.Enum) string {
	values := e.Descriptor().Values()
	value := values.ByNumber(e.Number())
	switch {
	case value == nil:
		return "?"
	case wordOverrides[value.Name()] != "":
		return wordOverrides[value.Name()]
	case e.Number() == 0:
		return "-"
	}
	// Every enum's zero value is <PREFIX>UNSPECIFIED.
	prefix := strings.TrimSuffix(string(values.Get(0).Name()), "UNSPECIFIED")
	name := strings.TrimPrefix(string(value.Name()), prefix)
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

// Parse returns the value of E whose Word is word, or E's zero value when no
// value has that word.
func Parse[E interface {
	~int32
	protoreflect.Enum
}](word string) E {
	values := E(0).Descriptor().Values()
	for i := range values.Len() {
		if e := E(values.Get(i).Number()); Word(e) == word {
			return e
		}
	}
	return 0
}
