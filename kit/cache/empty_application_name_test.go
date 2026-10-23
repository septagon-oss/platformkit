package cache

import "testing"

func TestEmptyApplicationNameIsRefusedByTheLegacyConstructorAndAcceptedByTheTypedOne(t *testing.T) {
	if c, err := New("", MemoryBackend()); err == nil || c != nil {
		t.Fatalf("New(\"\") = %v, %v; want a refusal", c, err)
	}
	c, err := NewFor("", MemoryBackend())
	if err != nil {
		t.Fatalf("NewFor(\"\") = %v", err)
	}
	_ = c.Close()
	defer func() {
		if recover() == nil {
			t.Fatal("Memory(\"\") must keep panicking")
		}
	}()
	Memory("")
}
