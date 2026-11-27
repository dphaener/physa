package adapter

import (
	"context"
	"errors"
	"testing"
)

type noUsageAgent struct{}

func (noUsageAgent) Name() string                                 { return "plain" }
func (noUsageAgent) Start(context.Context, Spec) (Session, error) { return nil, nil }

type reader struct {
	noUsageAgent
	u Usage
}

func (r reader) Usage(context.Context) (Usage, error) { return r.u, nil }

func TestBindingPicksHighestActive(t *testing.T) {
	u := Usage{Limits: []Limit{
		{Kind: "a", Percent: 90, Active: false},
		{Kind: "c", Percent: 40, Active: true},
		{Kind: "b", Percent: 40, Active: true},
		{Kind: "d", Percent: 10, Active: true},
	}}
	l, ok := u.Binding()
	if !ok || l.Kind != "b" {
		t.Errorf("Binding = %+v, %v; want b (tie by kind, inactive ignored)", l, ok)
	}
	u.Limits = append(u.Limits, Limit{Kind: "e", Percent: 70, Active: true})
	if l, _ := u.Binding(); l.Kind != "e" {
		t.Errorf("Binding = %+v; want e", l)
	}
	if _, ok := (Usage{Limits: []Limit{{Percent: 5}}}).Binding(); ok {
		t.Error("Binding found a limit with none active")
	}
}

func TestReadUsageNoReaderIsErrNoUsage(t *testing.T) {
	_, err := ReadUsage(context.Background(), noUsageAgent{})
	if !errors.Is(err, ErrNoUsage) {
		t.Fatalf("err = %v; want ErrNoUsage", err)
	}
}

func TestReadUsageNoActiveLimitIsErrNoUsage(t *testing.T) {
	_, err := ReadUsage(context.Background(), reader{u: Usage{Limits: []Limit{{Kind: "x", Percent: 1}}}})
	if !errors.Is(err, ErrNoUsage) {
		t.Fatalf("err = %v; want ErrNoUsage", err)
	}
	u, err := ReadUsage(context.Background(), reader{u: Usage{Limits: []Limit{{Kind: "x", Percent: 1, Active: true}}}})
	if err != nil || len(u.Limits) != 1 {
		t.Errorf("ReadUsage = %+v, %v", u, err)
	}
}
