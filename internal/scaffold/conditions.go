package scaffold

import (
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

type ConditionEvaluator struct {
	ctx *types.ScaffoldContext
}

func NewConditionEvaluator(ctx *types.ScaffoldContext) *ConditionEvaluator {
	return &ConditionEvaluator{ctx: ctx}
}

func (e *ConditionEvaluator) Evaluate(conditions map[string]interface{}) (bool, error) {
	return e.ctx.EvaluateCondition(conditions)
}
