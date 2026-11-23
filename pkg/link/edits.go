package link

import "github.com/startracex/build/pkg/jsedit"

type edit struct {
	start int
	end   int
	text  string
}

func applyEdits(src string, edits []edit) (string, error) {
	js := make([]jsedit.Edit, len(edits))
	for i, e := range edits {
		js[i] = jsedit.Edit{Start: e.start, End: e.end, Text: e.text}
	}
	return jsedit.ApplyEdits(src, js)
}
