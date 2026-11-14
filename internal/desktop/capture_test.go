package desktop

import "testing"

func TestNormalizeOptions(t *testing.T) {
	cases := []struct {
		width, quality         int
		wantWidth, wantQuality int
		wantErr                bool
	}{
		{0, 0, DefaultMaxWidth, DefaultQuality, false},
		{320, 35, 320, 35, false},
		{3840, 90, 3840, 90, false},
		{319, 70, 0, 0, true},
		{3841, 70, 0, 0, true},
		{1600, 34, 0, 0, true},
		{1600, 91, 0, 0, true},
	}
	for _, tc := range cases {
		w, q, err := normalizeOptions(tc.width, tc.quality)
		if (err != nil) != tc.wantErr || w != tc.wantWidth || q != tc.wantQuality {
			t.Errorf("normalizeOptions(%d,%d) = %d,%d,%v", tc.width, tc.quality, w, q, err)
		}
	}
}
