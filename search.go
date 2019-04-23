package query

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/txn2/ack"
	"github.com/txn2/es"
	"go.uber.org/zap"
)

// ModelSearchResults
type SearchResults struct {
	es.SearchResults
	Hits struct {
		Total    int      `json:"total"`
		MaxScore float64  `json:"max_score"`
		Hits     []Result `json:"hits"`
	} `json:"hits"`
}

// AccountSearchResultsAck
type SearchResultsAck struct {
	ack.Ack
	Payload SearchResults `json:"payload"`
}

// SearchModels
func (a *Api) SearchQueries(account string, searchObj *es.Obj) (int, SearchResults, error) {
	queryResults := &SearchResults{}

	code, err := a.Elastic.PostObjUnmarshal(fmt.Sprintf("%s-%s/_search", account, IdxQuery), searchObj, queryResults)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		return code, *queryResults, err
	}

	return code, *queryResults, nil
}

// SearchQueryHandler
func (a *Api) SearchQueryHandler(c *gin.Context) {
	ak := ack.Gin(c)

	obj := &es.Obj{}
	err := ak.UnmarshalPostAbort(obj)
	if err != nil {
		a.Logger.Error("Search failure.", zap.Error(err))
		return
	}

	// SearchModelsHandler must be security screened in
	// upstream middleware to protect account access.
	account := c.Param("account")

	code, esResult, err := a.SearchQueries(account, obj)
	if err != nil {
		a.Logger.Error("EsError", zap.Error(err))
		ak.SetPayloadType("EsError")
		ak.SetPayload("Error communicating with database.")
		ak.GinErrorAbort(500, "EsError", err.Error())
		return
	}

	if code >= 400 && code < 500 {
		ak.SetPayload(esResult)
		ak.GinErrorAbort(500, "SearchError", "There was a problem searching")
		return
	}

	ak.SetPayloadType("QuerySearchResults")
	ak.GinSend(esResult)
}
