package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/bouk/httprouter"
	"github.com/influxdata/chronograf"
)

type userRequest struct {
	ID       uint64   `json:"id,string"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Scheme   string   `json:"scheme"`
	Roles    []string `json:"roles"`
}

func (r *userRequest) ValidCreate() error {
	if r.Name == "" {
		return fmt.Errorf("Name required on Chronograf User request body")
	}
	if r.Provider == "" {
		return fmt.Errorf("Provider required on Chronograf User request body")
	}
	if r.Scheme == "" {
		return fmt.Errorf("Scheme required on Chronograf User request body")
	}
	return r.ValidRoles()
}

// TODO: Provide detailed error message
// TODO: Reconsider what fields should actually be updateable once this is more robust
func (r *userRequest) ValidUpdate() error {
	if r.Name == "" && r.Provider == "" && r.Scheme == "" && r.Roles == nil {
		return fmt.Errorf("No fields to update")
	}
	return r.ValidRoles()
}

func (r *userRequest) ValidRoles() error {
	if len(r.Roles) > 0 {
		for _, r := range r.Roles {
			if r != chronograf.ViewerRoleName && r != chronograf.EditorRoleName && r != chronograf.AdminRoleName {
				return fmt.Errorf("Unknown role %s. Valid roles are 'Viewer', 'Editor', 'Admin', and 'SuperAdmin'", r)
			}
		}
	}
	return nil
}

type userResponse struct {
	Links    selfLinks `json:"links"`
	ID       uint64    `json:"id,string"`
	Name     string    `json:"name"`
	Provider string    `json:"provider"`
	Scheme   string    `json:"scheme"`
	Roles    []string  `json:"roles"`
}

func newUserResponse(u *chronograf.User) *userResponse {
	roles := make([]string, len(u.Roles))
	for i, r := range u.Roles {
		roles[i] = r.Name
	}
	return &userResponse{
		ID:       u.ID,
		Name:     u.Name,
		Provider: u.Provider,
		Scheme:   u.Scheme,
		Roles:    roles,
		Links: selfLinks{
			Self: fmt.Sprintf("/chronograf/v1/users/%d", u.ID),
		},
	}
}

// ExplicatedRoles fills out a set of roles to include its members explicitly
func ExplicatedRoles(reqRoles []string) ([]chronograf.Role, error) {
	roles := make([]chronograf.Role, len(reqRoles))
	for i, r := range reqRoles {
		role, err := chronograf.RoleFromName(r)
		if err != nil {
			return nil, err
		}
		roles[i] = role
	}
	return roles, nil
}

type usersResponse struct {
	Links selfLinks       `json:"links"`
	Users []*userResponse `json:"users"`
}

func newUsersResponse(users []chronograf.User) *usersResponse {
	usersResp := make([]*userResponse, len(users))
	for i, user := range users {
		usersResp[i] = newUserResponse(&user)
	}
	sort.Slice(usersResp, func(i, j int) bool {
		return usersResp[i].ID < usersResp[j].ID
	})
	return &usersResponse{
		Users: usersResp,
		Links: selfLinks{
			Self: "/chronograf/v1/users",
		},
	}
}

// UserID retrieves a Chronograf user with ID from store
func (s *Service) UserID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id := httprouter.GetParamFromContext(ctx, "id")
	user, err := s.UsersStore.Get(ctx, id)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
		return
	}

	res := newUserResponse(user)
	encodeJSON(w, http.StatusOK, res, s.Logger)
}

// NewUser adds a new Chronograf user to store
func (s *Service) NewUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		invalidJSON(w, s.Logger)
		return
	}

	if err := req.ValidCreate(); err != nil {
		invalidData(w, err, s.Logger)
		return
	}

	roles, err := ExplicatedRoles(req.Roles)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
		return
	}

	ctx := r.Context()
	user := &chronograf.User{
		Name:     req.Name,
		Provider: req.Provider,
		Scheme:   req.Scheme,
		Roles:    roles,
	}

	res, err := s.UsersStore.Add(ctx, user)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
		return
	}

	cu := newUserResponse(res)
	location(w, cu.Links.Self)
	encodeJSON(w, http.StatusCreated, cu, s.Logger)
}

// RemoveUser deletes a Chronograf user from store
func (s *Service) RemoveUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httprouter.GetParamFromContext(ctx, "id")

	u, err := s.UsersStore.Get(ctx, id)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error(), s.Logger)
	}
	if err := s.UsersStore.Delete(ctx, u); err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateUser updates a Chronograf user in store
func (s *Service) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		invalidJSON(w, s.Logger)
		return
	}

	if err := req.ValidUpdate(); err != nil {
		invalidData(w, err, s.Logger)
		return
	}

	ctx := r.Context()
	id := httprouter.GetParamFromContext(ctx, "id")

	u, err := s.UsersStore.Get(ctx, id)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error(), s.Logger)
	}

	if req.Name != "" {
		u.Name = req.Name
	}
	if req.Provider != "" {
		u.Provider = req.Provider
	}
	if req.Scheme != "" {
		u.Scheme = req.Scheme
	}
	if req.Roles != nil {
		roles, err := ExplicatedRoles(req.Roles)
		if err != nil {
			Error(w, http.StatusBadRequest, err.Error(), s.Logger)
			return
		}
		u.Roles = roles
	}

	err = s.UsersStore.Update(ctx, u)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
		return
	}

	cu := newUserResponse(u)
	location(w, cu.Links.Self)
	encodeJSON(w, http.StatusOK, cu, s.Logger)
}

// Users retrieves all Chronograf users from store
func (s *Service) Users(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	users, err := s.UsersStore.All(ctx)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error(), s.Logger)
		return
	}

	res := newUsersResponse(users)
	encodeJSON(w, http.StatusOK, res, s.Logger)
}
