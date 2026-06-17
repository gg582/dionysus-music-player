package ui

import "testing"

func TestComposeHangulJamos(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ㅎㅏㄴㄱㅡㄹ", "한글"},
		{"ㅇㅏㄴㄴㅕㅇ", "안녕"},
		{"한글-ㅎㅏㄴㄱㅡㄹ", "한글-한글"},
		{"ㄷㅏㄹㄱ", "닭"},
		{"ㄱㅏㅁㅗ", "가모"},
		{"hello", "hello"},
		{"test.mp3", "test.mp3"},
		{"ㅎㅏㄴㄱㅡㄹ world", "한글 world"},
		{"가ㅁ", "가ㅁ"},
		{"", ""},
	}

	for _, c := range cases {
		got := composeHangulJamos(c.in)
		if got != c.want {
			t.Errorf("composeHangulJamos(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompatibilityJamo(t *testing.T) {
	jamos := []rune{'ㄱ', 'ㅏ', 'ㅣ', 'ㅎ', 'ㅃ'}
	for _, r := range jamos {
		if !compatibilityJamo(r) {
			t.Errorf("compatibilityJamo(%q) = false, want true", r)
		}
	}
	nonJamos := []rune{'a', '1', ' ', '가', 'ㅤ', '한'}
	for _, r := range nonJamos {
		if compatibilityJamo(r) {
			t.Errorf("compatibilityJamo(%q) = true, want false", r)
		}
	}
}
