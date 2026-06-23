package notification

import "testing"

func TestExpandTopicTemplate(t *testing.T) {
	cases := []struct {
		name  string
		topic string
		n     Notification
		want  string
	}{
		{
			name:  "no tokens passes through",
			topic: "matasan/alerts",
			n:     Notification{},
			want:  "matasan/alerts",
		},
		{
			name:  "data token resolved",
			topic: "matasan/alerts/{{cameraName}}/{{label}}",
			n:     Notification{Data: map[string]any{"cameraName": "Front Gate", "label": "fire"}},
			want:  "matasan/alerts/Front Gate/fire",
		},
		{
			name:  "top-level cameraId fallback",
			topic: "matasan/alerts/{{cameraId}}",
			n:     Notification{CameraId: 3, Data: map[string]any{"cameraName": "x"}},
			want:  "matasan/alerts/3",
		},
		{
			name:  "missing token collapses the empty level",
			topic: "matasan/alerts/{{cameraId}}",
			n:     Notification{}, // e.g. the Test notification (no camera)
			want:  "matasan/alerts",
		},
		{
			name:  "category/severity top-level tokens",
			topic: "matasan/{{category}}/{{severity}}",
			n:     Notification{Category: CategoryVisionAlert, Severity: Critical},
			want:  "matasan/vision.alert/critical",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandTopicTemplate(tc.topic, tc.n); got != tc.want {
				t.Errorf("expandTopicTemplate(%q) = %q, want %q", tc.topic, got, tc.want)
			}
		})
	}
}
