package store

import (
	"testing"
	"time"
)

func TestRevokeByNameMustBeUnique(t *testing.T) {
	s, err := New(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	a, _, _ := s.Add("iPhone", "fp-a", exp)
	s.Add("iPhone", "fp-b", exp)
	s.Add("iPad", "fp-c", exp)

	if _, err := s.Revoke("iphone"); err == nil {
		t.Fatal("revoking an ambiguous name should fail")
	}
	if s.Count() != 3 {
		t.Fatalf("nothing should be revoked, have %d devices", s.Count())
	}
	if _, err := s.Revoke(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Lookup("fp-a"); ok {
		t.Error("device revoked by ID is still present")
	}
	// Nu is "iPhone" wél uniek.
	if name, err := s.Revoke("iPhone"); err != nil || name != "iPhone" {
		t.Fatalf("Revoke(iPhone) = %q, %v", name, err)
	}
	if _, err := s.Revoke("nope"); err == nil {
		t.Error("revoking an unknown device should fail")
	}
}

func TestLookupReturnsCopy(t *testing.T) {
	s, _ := New(t.TempDir(), 5)
	s.Add("x", "fp", time.Now().Add(time.Hour))
	d, _ := s.Lookup("fp")
	d.Name = "changed"
	if d2, _ := s.Lookup("fp"); d2.Name != "x" {
		t.Error("Lookup leaked the stored pointer")
	}
}
