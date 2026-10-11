package news

import "context"

// marketFeeds is the free publisher-RSS source list for the Market News tab
// (SC-48). No key or account is needed for any of them. Adding a feed = one line;
// the aggregate (dedupe by URL, per-feed failure isolation, source attribution on
// every headline) is shared with the crypto news path.
//
// Headlines only: FT shows the title, the publisher name and a link to the original.
var marketFeeds = []rssSource{
	{Name: "CNBC", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114"},
	{Name: "MarketWatch", URL: "https://feeds.content.dowjones.io/public/rss/mw_topstories"},
	{Name: "Nasdaq", URL: "https://www.nasdaq.com/feed/rssoutbound?category=Markets"},
	{Name: "BBC Business", URL: "https://feeds.bbci.co.uk/news/business/rss.xml"},
}

// FetchMarketNewsRSS returns the merged market-news headlines from publisher RSS
// feeds, recorded in provider_health as "rss_market".
func FetchMarketNewsRSS(ctx context.Context) ([]Article, error) {
	return fetchRSSAggregate(ctx, "rss_market", marketFeeds, true)
}
