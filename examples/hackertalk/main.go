package main

import (
	"context"
	"fmt"

	lazyexp "github.com/mrwonko/lazyexp-go"
)

type franchiseNode struct {
	lazyexp.Node
	result giantbombFranchise
}

type franchiseNodeFetcher struct {
	ctx  context.Context
	url  string
	node *franchiseNode
}

func (fetcher franchiseNodeFetcher) Dependencies() lazyexp.Dependencies { return nil }

func (fetcher franchiseNodeFetcher) Fetch([]error) error {
	return giantbombQuery(fetcher.ctx, fetcher.url, franchiseFields, &fetcher.node.result)
}

func (fetcher franchiseNodeFetcher) String(complete bool) string {
	if complete {
		return fetcher.node.result.Results.Deck
	}
	return "Franchise " + fetcher.url
}

func fetchFranchise(ctx context.Context, url string) *franchiseNode {
	node := franchiseNode{}
	node.Node = lazyexp.NewNode(franchiseNodeFetcher{ctx, url, &node})
	return &node
}

type gameNodeFetcher struct {
	ctx       context.Context
	franchise *franchiseNode
	index     int
	dest      *giantbombGame
}

func (fetcher gameNodeFetcher) Dependencies() lazyexp.Dependencies { return nil }

func (fetcher gameNodeFetcher) Fetch([]error) error {
	if id := fetcher.franchise.result.Results.Games[fetcher.index].ID; id%2 == 1 {
		return fmt.Errorf("game ID %d is odd", id)
	}
	return giantbombQuery(fetcher.ctx, fetcher.franchise.result.Results.Games[fetcher.index].URL, gameFields, fetcher.dest)
}

func (fetcher gameNodeFetcher) String(complete bool) string {
	if complete {
		return fetcher.dest.Results.Name
	}
	return fmt.Sprintf("game %d", fetcher.franchise.result.Results.Games[fetcher.index].ID)
}

type gamesNode struct {
	lazyexp.Node
	result []giantbombGame
}

type gamesNodeFetcher struct {
	ctx       context.Context
	franchise *franchiseNode
	node      *gamesNode
}

func (fetcher gamesNodeFetcher) Dependencies() lazyexp.Dependencies {
	// depends on franchise
	return lazyexp.Dependencies{lazyexp.AbortOnError(fetcher.franchise)}
}

func (fetcher gamesNodeFetcher) Fetch([]error) (lazyexp.Node, error) {
	fetcher.node.result = make([]giantbombGame, len(fetcher.franchise.result.Results.Games))
	var games lazyexp.Dependencies
	for i := range fetcher.franchise.result.Results.Games {
		node := lazyexp.NewNode(gameNodeFetcher{fetcher.ctx, fetcher.franchise, i, &fetcher.node.result[i]})
		games = append(games, lazyexp.DiscardOnSuccess(node))
	}
	result := lazyexp.Join(games, nil)
	if err := writeGraph(result, "intermediate"); err != nil {
		return nil, err
	}
	return result, nil
}

func (fetcher gamesNodeFetcher) String(bool) string {
	return "games in " + fetcher.franchise.String()
}

func fetchGames(ctx context.Context, franchise *franchiseNode) *gamesNode {
	node := gamesNode{}
	node.Node = lazyexp.NewMetaNode(gamesNodeFetcher{ctx, franchise, &node})
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
