package glex

import "testing"

// Two structurally identical "generated" copies of the same lexicon, as would
// exist when two libraries each vendor and generate the same schema into
// their own packages.
type regTwinA struct {
	LexiconTypeID string    `json:"$type,omitempty"`
	Text          string    `json:"text"`
	Reply         *regTwinA `json:"reply,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
}

func (t *regTwinA) RecordTypeID() string { return "test.glex.registry.twin" }

type regTwinB struct {
	LexiconTypeID string    `json:"$type,omitempty"`
	Text          string    `json:"text"`
	Reply         *regTwinB `json:"reply,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
}

func (t *regTwinB) RecordTypeID() string { return "test.glex.registry.twin" }

// A conflicting definition: same ID, different shape.
type regConflict struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Text          int64  `json:"text"`
}

func (t *regConflict) RecordTypeID() string { return "test.glex.registry.twin" }

func TestRegisterTypeIdempotent(t *testing.T) {
	RegisterType("test.glex.registry.twin", &regTwinA{})
	// Exact same type again: fine.
	RegisterType("test.glex.registry.twin", &regTwinA{})
	// Structurally identical type from "another package": fine, first wins.
	RegisterType("test.glex.registry.twin", &regTwinB{})

	rec, err := NewFromType("test.glex.registry.twin")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.(*regTwinA); !ok {
		t.Errorf("first registration should win, got %T", rec)
	}
}

func TestRegisterTypeConflictPanics(t *testing.T) {
	RegisterType("test.glex.registry.twin", &regTwinA{})
	defer func() {
		if recover() == nil {
			t.Error("expected panic on structurally different registration for the same ID")
		}
	}()
	RegisterType("test.glex.registry.twin", &regConflict{})
}
