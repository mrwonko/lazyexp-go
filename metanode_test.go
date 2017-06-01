package lazyexp_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/mrwonko/lazyexp-go"
)

// This is an example of what cached database access with joins can look like

type Database struct {
	Users    map[int]DBUser
	Articles map[int]DBArticle
}

type DBUser struct {
	Name     string
	WishList []int
}

type DBArticle struct {
	Name string
}

type UserNode struct {
	lazyexp.Node
	Name     string
	WishList []*ArticleNode
}

func (n *UserNode) String() string {
	var buf bytes.Buffer
	buf.WriteString(n.Name)
	buf.WriteString(": ")
	for i, a := range n.WishList {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(a.Name)
	}
	return buf.String()
}

type ArticleNode struct {
	lazyexp.Node
	Name string
}

type NodeCache struct {
	sync.Mutex
	users    map[int]*UserNode
	articles map[int]*ArticleNode
	database Database
}

var errNotFound = errors.New("no entry found")

func (c *NodeCache) User(id int) *UserNode {
	c.Lock()
	defer c.Unlock()
	if user, ok := c.users[id]; ok {
		return user
	}
	user := &UserNode{}
	user.Node = lazyexp.NewMetaNode(nil, func([]error) (lazyexp.Node, error) {
		userInfo, ok := c.database.Users[id]
		if !ok {
			return nil, errNotFound
		}
		user.Name = userInfo.Name
		// fetch articles referenced in wish list
		articles := make([]*ArticleNode, len(userInfo.WishList))
		deps := make(lazyexp.Dependencies, len(userInfo.WishList))
		for i, id := range userInfo.WishList {
			article := c.Article(id)
			articles[i] = article
			deps[i] = lazyexp.ContinueOnError(article)
		}
		return lazyexp.NewNode(deps, func(errs []error) error {
			// only keep found articles
			for i, err := range errs {
				if err == nil {
					user.WishList = append(user.WishList, articles[i])
				}
			}
			return nil
		}), nil
	})
	c.users[id] = user
	return user
}

func (c *NodeCache) Article(id int) *ArticleNode {
	c.Lock()
	defer c.Unlock()
	if article, ok := c.articles[id]; ok {
		return article
	}
	article := &ArticleNode{}
	article.Node = lazyexp.NewNode(nil, func([]error) error {
		articleInfo, ok := c.database.Articles[id]
		if !ok {
			return errNotFound
		}
		article.Name = articleInfo.Name
		return nil
	})
	c.articles[id] = article
	return article
}

func TestMetaNode(t *testing.T) {
	var (
		cache = NodeCache{
			articles: map[int]*ArticleNode{},
			users:    map[int]*UserNode{},
			database: Database{
				Users: map[int]DBUser{
					0: DBUser{"Willi", []int{42, 1337, 9001}},
					1: DBUser{"Moritz", []int{1, 42}},
				},
				Articles: map[int]DBArticle{
					1:    DBArticle{Name: "PlayStation"},
					42:   DBArticle{Name: "Bloodborne"},
					1337: DBArticle{Name: "Effective Modern C++"},
				},
			},
		}
		willi  = cache.User(0)
		moritz = cache.User(1)
	)
	err := lazyexp.NewNode(
		lazyexp.Dependencies{lazyexp.AbortOnError(willi), lazyexp.AbortOnError(moritz)},
		func([]error) error { return nil },
	).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if got, expected := willi.String(), "Willi: Bloodborne, Effective Modern C++"; got != expected {
		t.Errorf("Expected user 0 to be %#v, got %#v", expected, got)
	}
	if got, expected := moritz.String(), "Moritz: PlayStation, Bloodborne"; got != expected {
		t.Errorf("Expected user 1 to be %#v, got %#v", expected, got)
	}
}
