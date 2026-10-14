package decision

import "testing"

func TestObservationCanonicalBytesAndDigest(t *testing.T) {
	obs := Observation{Facts: []Fact{
		{Source: SourceVCS, Key: "head", Value: "abc"},
		{Source: SourceVCS, Key: "clean", Value: "true"},
		{Source: SourceHost, Key: "bridge", Value: "installed"},
	}}
	want := `{
  "facts": [
    {
      "source": "VCS",
      "key": "head",
      "value": "abc"
    },
    {
      "source": "VCS",
      "key": "clean",
      "value": "true"
    },
    {
      "source": "HOST",
      "key": "bridge",
      "value": "installed"
    }
  ]
}
`
	for i := 0; i < 2; i++ {
		data, err := obs.CanonicalBytes()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("canonical bytes = %s, want %s", data, want)
		}
	}
	digest, err := obs.Digest()
	if err != nil {
		t.Fatal(err)
	}
	again, err := obs.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if digest != again {
		t.Fatal("same ordered observation must have the same digest")
	}
	for name, fact := range map[string]Fact{
		"source": {Source: SourceFile, Key: "head", Value: "abc"},
		"key":    {Source: SourceVCS, Key: "revision", Value: "abc"},
		"value":  {Source: SourceVCS, Key: "head", Value: "def"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := Observation{Facts: []Fact{fact, obs.Facts[1], obs.Facts[2]}}
			got, err := changed.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if got == digest {
				t.Fatalf("changing %s must change the observation digest", name)
			}
		})
	}
	empty := Observation{}
	data, err := empty.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"facts\": []\n}\n" {
		t.Fatalf("empty canonical bytes = %s", data)
	}
	emptyDigest, err := empty.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if emptyDigest == digest {
		t.Fatal("empty and non-empty observations must differ in digest")
	}
}

func TestFactSourceValid(t *testing.T) {
	for _, f := range []FactSource{SourceVCS, SourceFile, SourceHost, SourceLifecycle, SourceReceipt, SourceCapacity} {
		if !f.Valid() {
			t.Errorf("source %q should be valid", f)
		}
	}
	for _, f := range []FactSource{"", "vcs", "CAPACITY "} {
		if f.Valid() {
			t.Errorf("source %q should be invalid", f)
		}
	}
}
