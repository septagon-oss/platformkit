package design_test

// Review round 12. `Distance` is a mean over seventy-two readings — twelve
// identity tokens, two themes, three channels each — so the accent, which holds six
// of them, contributes one twelfth of the number `Colliding` reads. A pair cannot
// collide once its accents are 0.12 apart on that scale, because 0.12/12 alone
// reaches the 0.01 floor however alike every other token is. Half of that — 0.06,
// about fifteen levels of one channel — is already a different button, so this case
// asks, of every palette pair in a generated corpus whose accents are that far
// apart, that the register admit both clients. It measured 3734 such pairs and the
// closest sat 0.0140 apart, forty per cent above the floor; nine of the twelve
// tokens the mean averages are near-neutral surfaces and text, which a seed barely
// moves, so that margin is the accent's share and nothing else. It moves with
// `MinDistance`, with `identityTokens` and with how hard `enforce` walks an accent
// toward a neutral pole, which is why it is pinned rather than left as one
// round's measurement.
//
// What fails it: raising `MinDistance` past 0.014, dropping the accent from the
// tokens `Distance` reads, or a repair that flattens generated accents until two
// clients' buttons match — each of which makes the register refuse a client whose
// accent a reader could name. The remedy `Colliding`'s comment offers for a refused
// client ("names a brand colour") is measured in the review report, not pinned
// here: a brand colour fixes the accent's hue, which is the axis along which
// clients already collide.
//
// Measured at these bytes: 96 palettes, 3862 pairs at or above the threshold, 0
// refused, closest 0.0140 ("Client82 / Client95").

import (
	"fmt"
	"math"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// minVisibleAccent is how far apart two accents have to be for this case to call
// them two accents, in the units `Distance` reports: the mean absolute per-channel
// difference over both themes. It is half of the 0.12 at which the accent alone
// clears the register's floor, and a fifth of what the shipped palette's green
// measures against a client's crimson (0.21).
const minVisibleAccent = 0.06

func TestRegisterAdmitsTwoClientsWhoseAccentsDiffer(t *testing.T) {
	sectors := []string{"finance", "health", "education", "logistics", "energy", "retail", "legal", "media"}
	brands := []string{"#9f1239", "#1e40af", "#0f6f66", "#b45309", "#166534", "#6b21a8", "#334155", "#111111"}
	type client struct {
		name string
		pair design.Pair
	}
	var clients []client
	for i := 0; i < 96; i++ {
		seed := design.Seed{Sector: sectors[i%len(sectors)], Name: fmt.Sprintf("Client%d", i)}
		// A third of the corpus names a brand colour, because a client that already
		// owns one is the case the separation promise is hardest to keep.
		if i%3 == 0 {
			seed.Brand = brands[(i/3)%len(brands)]
		}
		pair, err := design.FromSeed(seed)
		if err != nil {
			t.Fatalf("seed %v was refused by its own generator: %v", seed, err)
		}
		clients = append(clients, client{name: seed.Name, pair: pair})
	}

	compared, refused, closest := 0, 0, 10.0
	var closestPair string
	for i := range clients {
		for j := i + 1; j < len(clients); j++ {
			if accentDistance(clients[i].pair, clients[j].pair) < minVisibleAccent {
				continue
			}
			compared++
			got := design.Distance(clients[i].pair, clients[j].pair)
			if design.Colliding(clients[i].pair, clients[j].pair) {
				refused++
				t.Errorf("%s and %s wear accents %.2f apart and Distance puts the two palettes %.4f apart, under the %.2f a reader needs: the register refuses a client whose accent a reader could name, and Distance does not report how far apart two pairs look",
					clients[i].name, clients[j].name, accentDistance(clients[i].pair, clients[j].pair), got, design.MinDistance)
				continue
			}
			if got < closest {
				closest, closestPair = got, clients[i].name+" / "+clients[j].name
			}
		}
	}
	if compared < 500 {
		t.Fatalf("the corpus offered %d pairs with visible accent difference; it no longer measures the thing this case is about", compared)
	}
	t.Logf("%d palettes, %d pairs with accent distance >= %.2f, %d refused, closest %.4f (%s) against the floor %.2f",
		len(clients), compared, minVisibleAccent, refused, closest, closestPair, design.MinDistance)
}

// accentDistance is how far the accent has moved between two pairs, in the same
// units Distance reports for all twelve identity tokens together: the mean absolute
// per-channel difference over both themes. Six readings, so one token's own
// distance is comparable with the whole measure.
func accentDistance(a, b design.Pair) float64 {
	const token = "--pk-color-accent-default"
	var total float64
	left, right := a.Both(), b.Both()
	for i := range left {
		lv, err := design.ResolveColors(left[i].Tokens(), nil)
		if err != nil {
			return 0
		}
		rv, err := design.ResolveColors(right[i].Tokens(), nil)
		if err != nil {
			return 0
		}
		l, r := lv[token], rv[token]
		for channel := 0; channel < 3; channel++ {
			total += math.Abs(l[channel] - r[channel])
		}
	}
	return total / 6
}
