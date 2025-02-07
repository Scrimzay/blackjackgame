package hand

import (
	"github.com/Scrimzay/blackjackgame/deck"
	"fmt"
	"strings"
)

type Hand []deck.Card

func (h Hand) String() string {
	strs := make([]string, len(h))
	for i := range h {
		strs[i] = h[i].String()
	}
	return strings.Join(strs, ", ")
}

func (h Hand) DealerString() string {
	return h[0].String() + ", **HIDDEN**"
}

func (h Hand) Score() int {
	minScore := h.MinScore()
	if minScore > 11 {
		return minScore
	}
	for _, c := range h {
		if c.Rank == deck.Ace {
			// ace is currently worth 1, changing to be 11
			return minScore + 10
		}
	}
	return minScore
}

func (h Hand) MinScore() int {
	score := 0
	for _, c := range h {
		score += min(int(c.Rank), 10)
	}

	return score
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Shuffle(gs GameState) GameState {
	ret := clone(gs)
	ret.Deck = deck.New(deck.Deck(3), deck.Shuffle)
	return ret
}

func Deal(gs GameState) GameState {
	ret := clone(gs)
	ret.Player = make(Hand, 0, 5)
	ret.Dealer = make(Hand, 0, 5)
	var card deck.Card
	for i := 0; i < 2; i++ {
		card, ret.Deck = draw(ret.Deck)
		ret.Player = append(ret.Player, card)
		card, ret.Deck = draw(ret.Deck)
		ret.Dealer = append(ret.Dealer, card)
	}
	ret.State = StatePlayerTurn
	// Debugging logs
	fmt.Printf("Deal: Player Hand: %v\n", ret.Player)
	fmt.Printf("Deal: Dealer Hand: %v\n", ret.Dealer)
	fmt.Printf("Deal: Remaining Deck: %d cards\n", len(ret.Deck))
	return ret
}

func Hit(gs GameState) GameState {
	ret := clone(gs)
	hand := ret.CurrentPlayer()
	var card deck.Card
	card, ret.Deck = draw(ret.Deck)
	// hand is a pointer, append takes a slice not pointer so use pointer
	*hand = append(*hand, card)
	if hand.Score() > 21 {
		ret.State = StateHandOver // end the game if a player busts
	}
	return ret
}

func Stand(gs GameState) GameState {
	ret := clone(gs)
	// incr increases the turn from PlayerTurn to DealerTurn
	ret.State++

	// dealers turn
	if ret.State == StateDealerTurn {
		for ret.Dealer.Score() < 17 || ret.Dealer.Score() == 17 && ret.Dealer.MinScore() != 17 {
			var card deck.Card
			card, ret.Deck = draw(ret.Deck)
			ret.Dealer = append(ret.Dealer, card)
		}
		ret.State = StateHandOver // end the hand after dealers turn
	}
	return ret
}

func EndHand(gs GameState) GameState {
	ret := clone(gs)
	pScore, dScore := ret.Player.Score(), ret.Dealer.Score()
	fmt.Println("===FINAL HANDS==")
	fmt.Println("Player:", ret.Player, "\nScore:", pScore)
	fmt.Println("Dealer:", ret.Dealer, "\nScore:", dScore)
	switch {
	case pScore > 21:
		fmt.Println("You busted.")
	case dScore > 21:
		fmt.Println("Dealer busted.")
	case pScore > dScore:
		fmt.Println("You win.")
	case dScore > pScore:
		fmt.Println("You lose.")
	case dScore == pScore:
		fmt.Println("It's a draw.")
	}
	fmt.Println()
	ret.Player = nil
	ret.Dealer = nil
	return ret
}

func draw(cards []deck.Card) (deck.Card, []deck.Card) {
	return cards[0], cards[1:]
}

type State int8

const (
	StatePlayerTurn State = iota
	StateDealerTurn
	StateHandOver
)

type GameState struct {
	Deck []deck.Card
	State State
	Player Hand
	Dealer Hand
	BetAmount float64
	BetCurrency string // added tot rack bet currency (cash/sol/etc)
}

func (gs *GameState) CurrentPlayer() *Hand {
	switch gs.State {
	case StatePlayerTurn:
		return &gs.Player
	case StateDealerTurn:
		return &gs.Dealer
	default:
		panic("It isn't currently any players' turn")
	}
}

func clone(gs GameState) GameState {
	ret := GameState {
		Deck: make([]deck.Card, len(gs.Deck)),
		State: gs.State,
		Player: make(Hand, len(gs.Player)),
		Dealer: make(Hand, len(gs.Dealer)),
		BetAmount: gs.BetAmount,
		BetCurrency: gs.BetCurrency,
	}
	copy(ret.Deck, gs.Deck)
	copy(ret.Player, gs.Player)
	copy(ret.Dealer, gs.Dealer)
	return ret
}