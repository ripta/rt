package streamdiff

import "fmt"

type ProgramEvaluationError struct {
	Inner      error
	Object     interface{}
	RawProgram string
}

func NewProgramEvaluationError(err error, prog string, obj interface{}) *ProgramEvaluationError {
	if err == nil {
		return nil
	}

	return &ProgramEvaluationError{
		Inner:      err,
		Object:     obj,
		RawProgram: prog,
	}
}

func (e *ProgramEvaluationError) Error() string {
	obj := fmt.Sprintf("%v", e.Object)
	if l := len(obj); l > 200 {
		obj = obj[:200] + "... " + fmt.Sprintf("(%d more bytes)", l-200)
	}

	return fmt.Sprintf("evaluating program: %s\n  Program: %s\n  Object: %v", e.Inner.Error(), e.RawProgram, obj)
}
