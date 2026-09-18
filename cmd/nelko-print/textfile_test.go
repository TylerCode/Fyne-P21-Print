package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTextLabelRoundTrip(t *testing.T) {
	want := textLabelFile{Format: "nelko-text-v1", Text: "Küche – Öl\nGröße: ½ Liter", LabelSize: "14x50mm", FontSize: 37,
		Orientation: "Vertical", TextInvert: true, WordBreakOnly: true, Density: 12, Copies: 2, Threshold: 140, Invert: true}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeTextLabel(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings lost: %#v", got)
	}
}

func TestTextLabelRejectsInvalidFiles(t *testing.T) {
	valid := textLabelFile{Format: "nelko-text-v1", LabelSize: "14x40mm", FontSize: 24, Orientation: "Horizontal", Density: 10, Copies: 1, Threshold: 128}
	for name, change := range map[string]func(*textLabelFile){
		"designer":    func(d *textLabelFile) { d.Format = "nelko-label-v1" },
		"size":        func(d *textLabelFile) { d.LabelSize = "unknown" },
		"font":        func(d *textLabelFile) { d.FontSize = 0 },
		"orientation": func(d *textLabelFile) { d.Orientation = "Diagonal" },
		"density":     func(d *textLabelFile) { d.Density = 16 },
		"copies":      func(d *textLabelFile) { d.Copies = -1 },
		"threshold":   func(d *textLabelFile) { d.Threshold = 256 },
	} {
		t.Run(name, func(t *testing.T) {
			d := valid
			change(&d)
			data, _ := json.Marshal(d)
			if _, err := decodeTextLabel(data); err == nil {
				t.Fatal("invalid file accepted")
			}
		})
	}
	if _, err := decodeTextLabel([]byte("broken")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
