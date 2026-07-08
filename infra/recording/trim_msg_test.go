package recording

import "testing"

func TestIsAccessDenied(t *testing.T) {
	denied := []string{
		"Optimize-Volume : Access denied \nActivity ID: {..} StorageWMI 40001",
		"Access is denied",
		"fstrim: /data: FITRIM ioctl failed: Operation not permitted",
	}
	for _, d := range denied {
		if !isAccessDenied(d) {
			t.Errorf("isAccessDenied(%q) = false, want true", d)
		}
	}
	if isAccessDenied("fstrim: the discard operation is not supported") {
		t.Errorf("unsupported-op message should not be treated as access denied")
	}
}
