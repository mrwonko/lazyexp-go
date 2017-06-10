package main

import (
	"context"
	"fmt"

	lazyexp "github.com/mrwonko/lazyexp-go"
)

// TODO: try interfaces instead of function arguments

type franchiseNode struct {
	lazyexp.Node
	result giantbombFranchise
}

func fetchFranchise(ctx context.Context, url string) *franchiseNode {
	node := franchiseNode{}
	node.Node = lazyexp.NewNode(nil, func(_ []error) error {
		return giantbombQuery(ctx, url, franchiseFields, &node.result)
	}, func(complete bool) string {
		if complete {
			return node.result.Results.Deck
		}
		return "Franchise " + url
	})
	return &node
}

type gamesNode struct {
	lazyexp.Node
	result []giantbombGame
}

func fetchGames(ctx context.Context, franchise *franchiseNode) *gamesNode {
	node := gamesNode{}
	node.Node = lazyexp.NewMetaNode(lazyexp.Dependencies{
		lazyexp.AbortOnError(franchise), // fetch the Franchise before this node can be fetched, abort on failure
	}, func(_ []error) (lazyexp.Node, error) {
		node.result = make([]giantbombGame, len(franchise.result.Results.Games))
		var games lazyexp.Dependencies
		for i := range franchise.result.Results.Games {
			j := i
			node := lazyexp.NewNode(nil, func(_ []error) error {
				if id := franchise.result.Results.Games[j].ID; id%2 == 1 {
					return fmt.Errorf("game ID %d is odd", id)
				}
				return giantbombQuery(ctx, franchise.result.Results.Games[j].URL, gameFields, &node.result[j])
			}, func(complete bool) string {
				if complete {
					return node.result[j].Results.Name
				}
				return fmt.Sprintf("game %d", franchise.result.Results.Games[j].ID)
			})
			games = append(games, lazyexp.DiscardOnSuccess(node))
		}
		result := lazyexp.NewNode(games, func(errs []error) error {
			fmt.Println(errs)
			return nil
		}, func(complete bool) string {
			return "games awaiter"
		})
		if err := writeGraph(result, "intermediate"); err != nil {
			return nil, err
		}
		return result, nil
	}, func(complete bool) string {
		return "all games"
	})
	return &node
}

func main() {
	ctx := context.Background()
	soulsFranchise := fetchFranchise(ctx, soulsFranchiseURL)
	games := fetchGames(ctx, soulsFranchise)

	if err := games.Fetch(); err != nil {
		fmt.Printf("Failed to download franchise: %s\n", err)
	}

	if err := writeGraph(games, "graph"); err != nil {
		fmt.Printf("Failed to write graph: %s\n", err)
	}

	if err := writeTimeline(games, "timeline"); err != nil {
		fmt.Printf("Failed to write timeline: %s\n", err)
	}
}
