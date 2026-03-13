package filtered_camera

import (
	"context"
	"testing"

	"go.viam.com/rdk/data"
	"go.viam.com/test"
)

func TestIsFromDataMgmt(t *testing.T) {
	t.Run("extra with FromDMString true", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{
			data.FromDMString: true,
		}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeTrue)
	})

	t.Run("extra with FromDMString false", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{
			data.FromDMString: false,
		}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeFalse)
	})

	t.Run("nil extra returns false", func(t *testing.T) {
		ctx := context.Background()
		result := IsFromDataMgmt(ctx, nil)
		test.That(t, result, test.ShouldBeFalse)
	})

	t.Run("empty extra returns false", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeFalse)
	})
}
