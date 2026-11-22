package workapi

// RepoIn is the input of the methods that list: the repo to read.
type RepoIn struct {
	Repo string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
}

// SlugIn is the input of the methods that show one idea or bet.
type SlugIn struct {
	Repo string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug string `json:"slug" desc:"The idea's or bet's slug."`
}

// AddIn is idea.add's input.
type AddIn struct {
	Repo   string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Text   string `json:"text" desc:"The idea, in a few plain words."`
	Slug   string `json:"slug,omitempty" desc:"The slug to give it; made from the text when left out."`
	Person string `json:"person" desc:"Who adds the idea; it belongs to them."`
}

// ShapeIn is idea.shape's input.
type ShapeIn struct {
	Repo     string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug     string `json:"slug" desc:"The idea to shape."`
	Markdown string `json:"markdown" desc:"The pitch as markdown: a ## heading for each of Problem, Appetite, Solution, Scope, Signal, Rabbit holes and No-gos."`
	Person   string `json:"person" desc:"Who shapes the pitch."`
}

// StartIn is bet.start's input.
type StartIn struct {
	Repo     string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug     string `json:"slug" desc:"The idea to bet on."`
	Appetite string `json:"appetite,omitempty" desc:"The appetite in tokens, such as 200k or 1.5m; the latest pitch's when left out."`
	Person   string `json:"person" desc:"The person who makes the bet."`
}

// StopIn is bet.stop's input.
type StopIn struct {
	Repo   string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Slug   string `json:"slug" desc:"The bet to stop."`
	Reason string `json:"reason,omitempty" desc:"Why it stops; \"stopped by <person>\" when left out."`
	Person string `json:"person" desc:"The person who stops the bet."`
}

// PitchOut is a pitch with its seven parts.
type PitchOut struct {
	Problem     string `json:"problem" desc:"The problem, in the user's words."`
	Appetite    int64  `json:"appetite" desc:"The appetite in tokens."`
	Solution    string `json:"solution" desc:"The solution, at the level of its parts."`
	Scope       string `json:"scope" desc:"What is in and what is out."`
	Signal      string `json:"signal" desc:"What would show it worked."`
	RabbitHoles string `json:"rabbit_holes" desc:"The places the work could sink."`
	NoGos       string `json:"no_gos" desc:"What the pitch rules out."`
	Person      string `json:"person" desc:"Who shaped it."`
	At          string `json:"at" desc:"When it was shaped, RFC 3339 in UTC."`
}

// IdeaOut is an idea and its facts. Pitch and Bets are set only where the
// method says so.
type IdeaOut struct {
	Slug      string    `json:"slug" desc:"The idea's slug."`
	Text      string    `json:"text" desc:"The idea."`
	Person    string    `json:"person" desc:"Who added it."`
	CreatedAt string    `json:"created_at" desc:"When it was added, RFC 3339 in UTC."`
	Pitches   int       `json:"pitches" desc:"How many times it was shaped."`
	Bet       string    `json:"bet,omitempty" desc:"The slug of its latest bet; left out when it has none."`
	BetState  string    `json:"bet_state,omitempty" desc:"The state of its latest bet: active, done or stopped."`
	Pitch     *PitchOut `json:"pitch,omitempty" desc:"Its latest pitch; set by idea.show and idea.shape when it has one."`
	Bets      []BetOut  `json:"bets,omitempty" desc:"Its bets, newest first; set by idea.show."`
}

// BetOut is a bet. Pitch is set only by bet.show, bet.start and bet.stop.
type BetOut struct {
	Slug       string    `json:"slug" desc:"The bet's slug."`
	Idea       string    `json:"idea" desc:"The slug of the idea it bets on."`
	IdeaText   string    `json:"idea_text" desc:"The idea."`
	Person     string    `json:"person" desc:"The person who made the bet."`
	Appetite   int64     `json:"appetite" desc:"The appetite in tokens."`
	Spent      int64     `json:"spent" desc:"The tokens spent so far."`
	State      string    `json:"state" desc:"active, done or stopped."`
	StopReason string    `json:"stop_reason,omitempty" desc:"Why it stopped; left out unless it did."`
	CreatedAt  string    `json:"created_at" desc:"When it was made, RFC 3339 in UTC."`
	StartedAt  string    `json:"started_at,omitempty" desc:"When it started, RFC 3339 in UTC."`
	EndedAt    string    `json:"ended_at,omitempty" desc:"When it ended, RFC 3339 in UTC; left out while active."`
	Pitch      *PitchOut `json:"pitch,omitempty" desc:"The pitch it was made on; left out when there was none."`
}
