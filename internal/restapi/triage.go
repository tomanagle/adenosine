package restapi

import (
	"encoding/base64"
	"net/http"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	searchservice "github.com/adenosine-dev/adenosine/internal/search"
	"github.com/adenosine-dev/adenosine/internal/triage"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func (handler *apiHandler) ListRepositoryLabels(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, params generated.ListRepositoryLabelsParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "repository-labels:" + string(owner) + "/" + string(slug)
	cursor, err := decodeCollectionCursor(encoded, scope)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	values, err := handler.deps.Triage.ListLabels(r.Context(), triageRoute(owner, slug), viewerDID, limit+1, cursor)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items, next, err := triageLabelPage(values, limit, scope)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.RepositoryLabelList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetRepositoryLabel(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.LabelPath) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Triage.GetLabel(r.Context(), triageRoute(owner, slug), string(id), viewerDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, repositoryLabelResponse(value))
}

func (handler *apiHandler) CreateRepositoryLabel(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, _ generated.CreateRepositoryLabelParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, ok := decodeLabelInput(handler, w, r)
	if !ok {
		return
	}
	value, err := handler.deps.Triage.CreateLabel(r.Context(), identity.accountDID, triageRoute(owner, slug), request)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+string(owner)+"/"+string(slug)+"/labels/"+value.RKey)
	writeJSON(w, http.StatusAccepted, generated.RepositoryLabelMutation{Label: repositoryLabelResponse(value), Projected: generated.RepositoryLabelMutationProjected(false)})
}

func (handler *apiHandler) UpdateRepositoryLabel(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.LabelPath, _ generated.UpdateRepositoryLabelParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, ok := decodeLabelInput(handler, w, r)
	if !ok {
		return
	}
	value, err := handler.deps.Triage.UpdateLabel(r.Context(), identity.accountDID, triageRoute(owner, slug), string(id), request)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.RepositoryLabelMutation{Label: repositoryLabelResponse(value), Projected: generated.RepositoryLabelMutationProjected(false)})
}

func (handler *apiHandler) DeleteRepositoryLabel(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.LabelPath, _ generated.DeleteRepositoryLabelParams) {
	identity, err := handler.requireSession(r, true)
	if err == nil {
		err = handler.deps.Triage.DeleteLabel(r.Context(), identity.accountDID, triageRoute(owner, slug), string(id))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) ListRepositoryMilestones(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, params generated.ListRepositoryMilestonesParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "repository-milestones:" + string(owner) + "/" + string(slug)
	cursor, err := decodeCollectionCursor(encoded, scope)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	values, err := handler.deps.Triage.ListMilestones(r.Context(), triageRoute(owner, slug), viewerDID, limit+1, cursor)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items, next, err := triageMilestonePage(values, limit, scope)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.RepositoryMilestoneList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetRepositoryMilestone(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.MilestonePath) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Triage.GetMilestone(r.Context(), triageRoute(owner, slug), string(id), viewerDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, repositoryMilestoneResponse(value))
}

func (handler *apiHandler) CreateRepositoryMilestone(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, _ generated.CreateRepositoryMilestoneParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, ok := decodeMilestoneInput(handler, w, r)
	if !ok {
		return
	}
	value, err := handler.deps.Triage.CreateMilestone(r.Context(), identity.accountDID, triageRoute(owner, slug), request)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+string(owner)+"/"+string(slug)+"/milestones/"+value.RKey)
	writeJSON(w, http.StatusAccepted, generated.RepositoryMilestoneMutation{Milestone: repositoryMilestoneResponse(value), Projected: generated.RepositoryMilestoneMutationProjected(false)})
}

func (handler *apiHandler) UpdateRepositoryMilestone(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.MilestonePath, _ generated.UpdateRepositoryMilestoneParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, ok := decodeMilestoneInput(handler, w, r)
	if !ok {
		return
	}
	value, err := handler.deps.Triage.UpdateMilestone(r.Context(), identity.accountDID, triageRoute(owner, slug), string(id), request)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.RepositoryMilestoneMutation{Milestone: repositoryMilestoneResponse(value), Projected: generated.RepositoryMilestoneMutationProjected(false)})
}

func (handler *apiHandler) DeleteRepositoryMilestone(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, id generated.MilestonePath, _ generated.DeleteRepositoryMilestoneParams) {
	identity, err := handler.requireSession(r, true)
	if err == nil {
		err = handler.deps.Triage.DeleteMilestone(r.Context(), identity.accountDID, triageRoute(owner, slug), string(id))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) GetIssueTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath) {
	handler.getSubjectTriage(w, r, owner, slug, subject, triage.SubjectIssue)
}

func (handler *apiHandler) PutIssueTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath, _ generated.PutIssueTriageParams) {
	handler.putSubjectTriage(w, r, owner, slug, subject, triage.SubjectIssue)
}

func (handler *apiHandler) DeleteIssueTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath, _ generated.DeleteIssueTriageParams) {
	handler.deleteSubjectTriage(w, r, owner, slug, subject, triage.SubjectIssue)
}

func (handler *apiHandler) GetPullRequestTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath) {
	handler.getSubjectTriage(w, r, owner, slug, subject, triage.SubjectPullRequest)
}

func (handler *apiHandler) PutPullRequestTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath, _ generated.PutPullRequestTriageParams) {
	handler.putSubjectTriage(w, r, owner, slug, subject, triage.SubjectPullRequest)
}

func (handler *apiHandler) DeletePullRequestTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, subject generated.SubjectPath, _ generated.DeletePullRequestTriageParams) {
	handler.deleteSubjectTriage(w, r, owner, slug, subject, triage.SubjectPullRequest)
}

func (handler *apiHandler) getSubjectTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, encoded generated.SubjectPath, kind triage.SubjectKind) {
	subject, err := decodeTriageSubject(encoded)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Triage.GetMetadata(r.Context(), triageRoute(owner, slug), kind, subject, viewerDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, subjectTriageResponse(value))
}

func (handler *apiHandler) putSubjectTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, encoded generated.SubjectPath, kind triage.SubjectKind) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	subject, err := decodeTriageSubject(encoded)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	var request generated.SubjectTriageInput
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	milestoneID := ""
	if request.MilestoneId != nil {
		milestoneID = *request.MilestoneId
	}
	value, err := handler.deps.Triage.PutMetadata(r.Context(), identity.accountDID, triageRoute(owner, slug), kind, subject, triage.MetadataInput{LabelIDs: request.LabelIds, AssigneeDIDs: request.AssigneeDids, MilestoneID: milestoneID})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.SubjectTriageMutation{Triage: subjectTriageResponse(value), Projected: generated.SubjectTriageMutationProjected(false)})
}

func (handler *apiHandler) deleteSubjectTriage(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, encoded generated.SubjectPath, kind triage.SubjectKind) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	subject, err := decodeTriageSubject(encoded)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	err = handler.deps.Triage.DeleteMetadata(r.Context(), identity.accountDID, triageRoute(owner, slug), kind, subject)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func decodeLabelInput(handler *apiHandler, w http.ResponseWriter, r *http.Request) (triage.LabelInput, bool) {
	var request generated.RepositoryLabelInput
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return triage.LabelInput{}, false
	}
	return triage.LabelInput{Name: request.Name, Color: request.Color, Description: request.Description}, true
}

func decodeMilestoneInput(handler *apiHandler, w http.ResponseWriter, r *http.Request) (triage.MilestoneInput, bool) {
	var request generated.RepositoryMilestoneInput
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return triage.MilestoneInput{}, false
	}
	return triage.MilestoneInput{Title: request.Title, Description: request.Description, State: triage.MilestoneState(request.State), DueAt: request.DueAt}, true
}

func triageRoute(owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath) triage.RepositoryRoute {
	return triage.RepositoryRoute{Owner: string(owner), Slug: string(slug)}
}

func decodeTriageSubject(value generated.SubjectPath) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(string(value))
	if err != nil {
		return "", err
	}
	if len(decoded) == 0 || len(decoded) > 3072 {
		return "", errInvalidCursor
	}
	return string(decoded), nil
}

func triageLabelPage(values []triage.Label, limit int, scope string) ([]generated.RepositoryLabel, *string, error) {
	hasNext := len(values) > limit
	if hasNext {
		values = values[:limit]
	}
	items := make([]generated.RepositoryLabel, len(values))
	for index, value := range values {
		items[index] = repositoryLabelResponse(value)
	}
	if !hasNext || len(values) == 0 {
		return items, nil, nil
	}
	next, err := encodeCollectionCursor(scope, values[len(values)-1].URI)
	return items, next, err
}

func triageMilestonePage(values []triage.Milestone, limit int, scope string) ([]generated.RepositoryMilestone, *string, error) {
	hasNext := len(values) > limit
	if hasNext {
		values = values[:limit]
	}
	items := make([]generated.RepositoryMilestone, len(values))
	for index, value := range values {
		items[index] = repositoryMilestoneResponse(value)
	}
	if !hasNext || len(values) == 0 {
		return items, nil, nil
	}
	next, err := encodeCollectionCursor(scope, values[len(values)-1].URI)
	return items, next, err
}

func repositoryLabelResponse(value triage.Label) generated.RepositoryLabel {
	return generated.RepositoryLabel{Id: value.RKey, Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID, Name: value.Name, Color: value.Color, Description: value.Description, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: timePointerUnlessZero(value.IndexedAt)}
}

func repositoryMilestoneResponse(value triage.Milestone) generated.RepositoryMilestone {
	return generated.RepositoryMilestone{Id: value.RKey, Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID, Title: value.Title, Description: value.Description, State: generated.RepositoryMilestoneState(value.State), DueAt: value.DueAt, ClosedAt: value.ClosedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: timePointerUnlessZero(value.IndexedAt)}
}

func subjectTriageResponse(value triage.Metadata) generated.SubjectTriage {
	labels := make([]generated.RepositoryLabel, len(value.Labels))
	for index, label := range value.Labels {
		labels[index] = repositoryLabelResponse(label)
	}
	assignees := make([]generated.TriageAssignee, len(value.Assignees))
	for index, assignee := range value.Assignees {
		assignees[index] = generated.TriageAssignee{Did: assignee.DID, Handle: assignee.Handle, DisplayName: assignee.DisplayName}
	}
	labelIDs := recordKeys(value.LabelURIs)
	milestoneID := recordKeyPointer(value.MilestoneURI)
	response := generated.SubjectTriage{Uri: pointerUnlessEmpty(value.URI), Cid: pointerUnlessEmpty(value.CID), AuthorDid: pointerUnlessEmpty(value.AuthorDID), SubjectUri: value.Subject.URI, SubjectCid: value.Subject.CID, SubjectKind: generated.SubjectTriageSubjectKind(value.Kind), RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID, LabelIds: labelIDs, AssigneeDids: append([]string{}, value.AssigneeDIDs...), MilestoneId: milestoneID, Labels: labels, Assignees: assignees, CreatedAt: timePointerUnlessZero(value.CreatedAt), UpdatedAt: timePointerUnlessZero(value.UpdatedAt), IndexedAt: timePointerUnlessZero(value.IndexedAt)}
	if value.Milestone != nil {
		milestone := repositoryMilestoneResponse(*value.Milestone)
		response.Milestone = &milestone
	}
	return response
}

func recordKeys(uris []string) []string {
	values := make([]string, 0, len(uris))
	for _, uri := range uris {
		if parsed, err := syntax.ParseATURI(uri); err == nil {
			values = append(values, parsed.RecordKey().String())
		}
	}
	return values
}

func recordKeyPointer(uri string) *string {
	if uri == "" {
		return nil
	}
	values := recordKeys([]string{uri})
	if len(values) != 1 {
		return nil
	}
	return &values[0]
}

func timePointerUnlessZero(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func issueTriageFilter(params generated.GetIssuesParams) triageFilter {
	filter := triageFilter{}
	if params.State != nil {
		filter.State = string(*params.State)
	}
	if params.Label != nil {
		filter.Label = string(*params.Label)
	}
	if params.Assignee != nil {
		filter.Assignee = string(*params.Assignee)
	}
	if params.Milestone != nil {
		filter.Milestone = string(*params.Milestone)
	}
	return filter
}

func pullRequestTriageFilter(params generated.ListPullRequestsParams) triageFilter {
	filter := triageFilter{}
	if params.State != nil {
		filter.State = string(*params.State)
	}
	if params.Label != nil {
		filter.Label = string(*params.Label)
	}
	if params.Assignee != nil {
		filter.Assignee = string(*params.Assignee)
	}
	if params.Milestone != nil {
		filter.Milestone = string(*params.Milestone)
	}
	return filter
}

// Alias keeps the REST conversion local while the validation and cursor binding remain in search.
type triageFilter = searchservice.TriageFilter
