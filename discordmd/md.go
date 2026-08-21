package discordmd

import (
	"sync"

	"github.com/ayn2op/arikawa/v3/discord"
	"github.com/ayn2op/arikawa/v3/state/store"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	messageCtx  = parser.NewContextKey()
	sessionCtx  = parser.NewContextKey()
	parserPools = [2]sync.Pool{
		{New: func() any { return newPooledParser(false) }},
		{New: func() any { return newPooledParser(true) }},
	}
)

type pooledParser struct {
	parser.Parser
	emoji *emoji
}

func newPooledParser(links bool) *pooledParser {
	emojiParser := &emoji{}
	inlines := inlineParsers(emojiParser)
	if links {
		inlines = append(inlines, util.Prioritized(parser.NewLinkParser(), 600))
	}
	return &pooledParser{
		Parser: parser.NewParser(
			parser.WithBlockParsers(BlockParsers()...),
			parser.WithInlineParsers(inlines...),
		),
		emoji: emojiParser,
	}
}

func parse(content []byte, links bool, opts ...parser.ParseOption) ast.Node {
	pool := &parserPools[0]
	if links {
		pool = &parserPools[1]
	}
	p := pool.Get().(*pooledParser)
	*p.emoji = emoji{}
	root := p.Parse(text.NewReader(content), opts...)
	pool.Put(p)
	return root
}

// ParseWithMessage parses the given byte slice with the Discord state and the
// Message as source for the ast nodes. If msg is false, then links will also be
// parsed (accordingly to embeds and webhooks, normal messages don't have
// links).
func ParseWithMessage(b []byte, s store.Cabinet, m *discord.Message, msg bool) ast.Node {
	// Context to pass down messages:
	ctx := parser.NewContext()
	ctx.Set(messageCtx, m)
	ctx.Set(sessionCtx, &s)

	return parse(b, !msg, parser.WithContext(ctx))
}

// Parse parses the given byte slice with extra options. It does not parse
// links.
func Parse(content []byte, opts ...parser.ParseOption) ast.Node {
	return parse(content, false, opts...)
}

func getMessage(pc parser.Context) *discord.Message {
	if v := pc.Get(messageCtx); v != nil {
		return v.(*discord.Message)
	}
	return nil
}
func getSession(pc parser.Context) *store.Cabinet {
	if v := pc.Get(sessionCtx); v != nil {
		return v.(*store.Cabinet)
	}
	return nil
}
