package services

import "testing"

// TestFaceVectorRoundTrip proves an embedding survives encode->store->decode unchanged (the base64
// TEXT storage that keeps the gallery portable across sqlite/MariaDB/Postgres), with no cipher.
func TestFaceVectorRoundTrip(t *testing.T) {
	s := &FaceGalleryService{}
	in := []float32{0.1, -0.2, 0.3333, 1.0, -1.0, 0}
	enc, err := s.encodeVector(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := s.decodeVector(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len %d != %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("vec[%d] = %v, want %v", i, out[i], in[i])
		}
	}
}

// TestPickEnrollFace proves enrollment fails LOUDLY on zero/multiple/low-quality/tiny faces — a bad
// enrollment silently poisons every future match, so these must error, not be stored.
func TestPickEnrollFace(t *testing.T) {
	cases := map[string]struct {
		faces   []DetectedFace
		wantErr error
	}{
		"no face":     {faces: nil, wantErr: ErrNoFace},
		"two faces":   {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.3, 0.3}}, {Quality: 0.9}}, wantErr: ErrMultipleFaces},
		"low quality": {faces: []DetectedFace{{Quality: 0.2, Box: [4]float64{0, 0, 0.3, 0.3}}}, wantErr: ErrNoFace},
		"too small":   {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.005, 0.005}}}, wantErr: ErrFaceTooSmall},
		"one good":    {faces: []DetectedFace{{Quality: 0.9, Box: [4]float64{0, 0, 0.3, 0.3}}}, wantErr: nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pickEnrollFace(c.faces)
			if err != c.wantErr {
				t.Errorf("pickEnrollFace error = %v, want %v", err, c.wantErr)
			}
		})
	}
}
