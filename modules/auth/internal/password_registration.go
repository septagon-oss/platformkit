package internal

import (
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// Both password-first policies validate the same visitor input. Their user
// lifecycle commands and activation proofs remain distinct.
type passwordRegistrationInput struct {
	Body struct {
		Email         string `json:"email" minLength:"1" maxLength:"320" format:"email" doc:"Your email address"`
		DisplayName   string `json:"displayName" minLength:"1" maxLength:"200" doc:"Your full name"`
		Password      string `json:"password" minLength:"12" maxLength:"256" writeOnly:"true" doc:"At least twelve characters; spaces are allowed"`
		Confirmation  string `json:"confirmation" minLength:"12" maxLength:"256" writeOnly:"true" doc:"Repeat the password exactly"`
		TermsAccepted bool   `json:"termsAccepted" doc:"You have read and accept this application's terms"`
	}
}

func (in *passwordRegistrationInput) validate() error {
	switch {
	case !in.Body.TermsAccepted:
		return problem.New(http.StatusUnprocessableEntity, "accept the terms before requesting an account")
	case in.Body.Password != in.Body.Confirmation:
		return problem.New(http.StatusUnprocessableEntity, "the password and confirmation must match")
	case strings.TrimSpace(in.Body.DisplayName) == "":
		return problem.New(http.StatusUnprocessableEntity, "enter your full name")
	}
	return nil
}
