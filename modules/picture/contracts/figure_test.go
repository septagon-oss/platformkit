package contracts_test

import (
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
	"testing"
)

type pureFigure struct{}

func (pureFigure) DeclareFigure(name, source string, ids []string) (contracts.Figure, error) {
	return contracts.CompileFigure(name, source, ids)
}
func TestFigureAdaptersShareTheDecision(t *testing.T) {
	t.Run("pure", func(t *testing.T) {
		picturetest.RunFigures(t, func() picturetest.FigureDecision { return pureFigure{} })
	})
	t.Run("fake", func(t *testing.T) {
		picturetest.RunFigures(t, func() picturetest.FigureDecision { return &picturetest.Fake{} })
	})
}
