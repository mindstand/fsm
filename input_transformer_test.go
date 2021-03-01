package fsm

import (
	"testing"
)

var (
	validIntents = []*Intent{
		SampleIntent,
	}
)

func TestCleanInput(t *testing.T) {
	if CleanInput("Hello!") != "hello" {
		t.Fail()
	}
	if CleanInput("Hello  World") != "hello world" {
		t.Fail()
	}
	if CleanInput("Hello, World") != "hello world" {
		t.Fail()
	}
}

func TestValidTextInputTransformer(t *testing.T) {
	intent, params := TextInputTransformer("I am a 29 year old male.", validIntents)
	if intent != SampleIntent {
		t.Fail()
	}
	if params["gender"] != "male" {
		t.Fail()
	}
	if params["age"] != "29" {
		t.Fail()
	}
}

func TestInvalidTextInputTransformer(t *testing.T) {
	intent, params := TextInputTransformer("hello world", validIntents)
	if intent != nil {
		t.Fail()
	}
	if len(params) != 0 {
		t.Fail()
	}
}
