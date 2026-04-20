package types

type Subject struct {
	Title     string   `json:"title"`
	Directors []string `json:"directors"`
	TicketURL string   `json:"ticketUrl"`
}

type ReviewItem struct {
	IsCriticsPick bool    `json:"isCriticsPick"`
	Subject       Subject `json:"subject"`
}

type Node struct {
	FirstPublished string       `json:"firstPublished"`
	Summary        string       `json:"summary"`
	URL            string       `json:"url"`
	ReviewItems    []ReviewItem `json:"reviewItems"`
}

type Edge struct {
	Node Node `json:"node"`
}

type GraphQLResponse struct {
	Data struct {
		ContentSearch struct {
			Hits struct {
				Edges []Edge `json:"edges"`
			} `json:"hits"`
		} `json:"contentSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type Entry struct {
	Timestamp           string   `json:"timestamp"`
	Source              string   `json:"source"`
	Title               string   `json:"title"`
	Directors           []string `json:"directors"`
	IMDBID              *string  `json:"imdb_id"`
	ReviewPublishDate   string   `json:"review_publish_date"`
	ReviewLeadParagraph string   `json:"review_lead_paragraph"`
	ReviewLink          string   `json:"review_link"`
}
