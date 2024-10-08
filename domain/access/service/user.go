// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/juju/errors"
	"golang.org/x/crypto/nacl/secretbox"

	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/user"
	accesserrors "github.com/juju/juju/domain/access/errors"
	"github.com/juju/juju/internal/auth"
)

// UserService provides the API for working with users.
type UserService struct {
	st UserState
}

// NewUserService returns a new UserService for interacting with the underlying user
// state.
func NewUserService(st UserState) *UserService {
	return &UserService{
		st: st,
	}
}

// GetAllUsers will retrieve all users with authentication information
// (last login, disabled) from the database. If no users exist an empty slice
// will be returned.
func (s *UserService) GetAllUsers(ctx context.Context, includeDisabled bool) ([]user.User, error) {
	usrs, err := s.st.GetAllUsers(ctx, includeDisabled)
	if err != nil {
		return nil, errors.Annotate(err, "getting all users with auth info")
	}
	return usrs, nil
}

// GetUser will find and return the user with UUID. If there is no
// user for the UUID then an error that satisfies accesserrors.NotFound will
// be returned.
func (s *UserService) GetUser(
	ctx context.Context,
	uuid user.UUID,
) (user.User, error) {
	if err := uuid.Validate(); err != nil {
		return user.User{}, errors.Annotatef(accesserrors.UserUUIDNotValid, "validating uuid %q", uuid)
	}

	usr, err := s.st.GetUser(ctx, uuid)
	if err != nil {
		return user.User{}, errors.Annotatef(err, "getting user for uuid %q", uuid)
	}

	return usr, nil
}

// GetUserByName will find and return the user associated with name. If there is no
// user for the user name then an error that satisfies accesserrors.NotFound will
// be returned. If supplied with an invalid user name then an error that satisfies
// accesserrors.UserNameNotValid will be returned.
//
// GetUserByName will not return users that have been previously removed.
func (s *UserService) GetUserByName(
	ctx context.Context,
	name user.Name,
) (user.User, error) {
	if name.IsZero() {
		return user.User{}, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	usr, err := s.st.GetUserByName(ctx, name)
	if err != nil {
		return user.User{}, errors.Annotatef(err, "getting user %q", name)
	}

	return usr, nil
}

// GetUserByAuth will find and return the user with UUID. If there is no
// user for the name and password, then an error that satisfies
// accesserrors.NotFound will be returned. If supplied with an invalid user name
// then an error that satisfies accesserrors.UserNameNotValid will be returned.
// It will not return users that have been previously removed.
func (s *UserService) GetUserByAuth(
	ctx context.Context,
	name user.Name,
	password auth.Password,
) (user.User, error) {
	if name.IsZero() {
		return user.User{}, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := password.Validate(); err != nil {
		return user.User{}, errors.Trace(err)
	}

	usr, err := s.st.GetUserByAuth(ctx, name, password)
	if err != nil {
		// We only need to ensure destruction on an error.
		// The happy path hashes the password in state,
		// and in so doing destroys it.
		password.Destroy()
		return user.User{}, errors.Annotatef(err, "getting user %q", name)
	}

	return usr, nil
}

// AddUser will add a new user to the database and return the UUID of the
// user if successful. If no password is set in the incoming argument,
// the user will be added with an activation key.
// The following error types are possible from this function:
//   - accesserrors.UserNameNotValid: When the username supplied is not valid.
//   - accesserrors.UserAlreadyExists: If a user with the supplied name already exists.
//   - accesserrors.CreatorUUIDNotFound: If a creator has been supplied for the user
//     and the creator does not exist.
//   - auth.ErrPasswordNotValid: If the password supplied is not valid.
func (s *UserService) AddUser(ctx context.Context, arg AddUserArg) (user.UUID, []byte, error) {
	if arg.Name.IsZero() {
		return "", nil, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}
	if !arg.Name.IsLocal() {
		return "", nil, errors.Annotatef(accesserrors.UserNameNotValid, "cannot add external user %q", arg.Name)
	}
	if err := arg.CreatorUUID.Validate(); err != nil {
		return "", nil, errors.Annotatef(err, "validating creator UUID %q", arg.CreatorUUID)
	}

	if err := arg.Permission.Validate(); err != nil {
		return "", nil, fmt.Errorf("validating permission %q: %w: %w", arg.Permission, err, accesserrors.PermissionNotValid)
	}

	if arg.UUID.String() == "" {
		var err error
		if arg.UUID, err = user.NewUUID(); err != nil {
			return "", nil, errors.Annotatef(err, "generating UUID for user %q", arg.Name)
		}
	} else if err := arg.UUID.Validate(); err != nil {
		return "", nil, errors.Annotatef(err, "validating user UUID %q", arg.UUID)
	}

	var key []byte
	var err error
	if arg.Password != nil {
		err = s.addUserWithPassword(ctx, arg)
	} else {
		key, err = s.addUserWithActivationKey(ctx, arg)
	}
	if err != nil {
		return "", nil, errors.Trace(err)
	}

	return arg.UUID, key, nil
}

func (s *UserService) addUserWithPassword(ctx context.Context, arg AddUserArg) error {
	if err := arg.Password.Validate(); err != nil {
		return errors.Trace(err)
	}

	salt, err := auth.NewSalt()
	if err != nil {
		return errors.Trace(err)
	}

	hash, err := auth.HashPassword(*arg.Password, salt)
	if err != nil {
		return errors.Trace(err)
	}

	err = s.st.AddUserWithPasswordHash(ctx, arg.UUID, arg.Name, arg.DisplayName, arg.CreatorUUID, arg.Permission, hash, salt)
	return errors.Trace(err)
}

func (s *UserService) addUserWithActivationKey(ctx context.Context, arg AddUserArg) ([]byte, error) {
	key, err := generateActivationKey()
	if err != nil {
		return nil, errors.Trace(err)
	}

	if err = s.st.AddUserWithActivationKey(ctx, arg.UUID, arg.Name, arg.DisplayName, arg.CreatorUUID, arg.Permission, key); err != nil {
		return nil, errors.Trace(err)
	}
	return key, nil
}

// AddExternalUser adds a new external user to the database and does not set a
// password or activation key.
// The following error types are possible from this function:
//   - accesserrors.UserNameNotValid: When the username supplied is not valid.
//   - accesserrors.UserAlreadyExists: If a user with the supplied name already exists.
//   - accesserrors.CreatorUUIDNotFound: If the creator supplied for the user
//     does not exist.
func (s *UserService) AddExternalUser(ctx context.Context, name user.Name, displayName string, creatorUUID user.UUID) error {
	if name.IsLocal() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "cannot use add external user method to add local user")
	}

	uuid, err := user.NewUUID()
	if err != nil {
		return errors.Annotate(err, "generating user UUID")
	}
	err = s.st.AddUser(ctx, uuid, name, displayName, true, creatorUUID)
	return err
}

// RemoveUser marks the user as removed and removes any credentials or
// activation codes for the current users. Once a user is removed they are no
// longer usable in Juju and should never be un removed.
// The following error types are possible from this function:
// - accesserrors.UserNameNotValid: When the username supplied is not valid.
// - accesserrors.NotFound: If no user by the given UUID exists.
func (s *UserService) RemoveUser(ctx context.Context, name user.Name) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}
	if err := s.st.RemoveUser(ctx, name); err != nil {
		return errors.Annotatef(err, "removing user for %q", name)
	}
	return nil
}

// SetPassword changes the users password to the new value and removes any
// active activation keys for the users.
// The following error types are possible from this function:
//   - accesserrors.UserNameNotValid: When the username supplied is not valid.
//   - accesserrors.NotFound: If no user by the given name exists.
//   - internal/auth.ErrPasswordNotValid: If the password supplied is not valid.
func (s *UserService) SetPassword(ctx context.Context, name user.Name, pass auth.Password) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := pass.Validate(); err != nil {
		return errors.Trace(err)
	}

	err := s.setPassword(ctx, name, pass)
	return errors.Trace(err)
}

// ResetPassword will remove any active passwords for a user and generate a new
// activation key for the user to use to set a new password.
// The following error types are possible from this function:
// - accesserrors.UserNameNotValid: When the username supplied is not valid.
// - accesserrors.NotFound: If no user by the given UUID exists.
func (s *UserService) ResetPassword(ctx context.Context, name user.Name) ([]byte, error) {
	if name.IsZero() {
		return nil, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	activationKey, err := generateActivationKey()
	if err != nil {
		return nil, errors.Trace(err)
	}

	if err = s.st.SetActivationKey(ctx, name, activationKey); err != nil {
		return nil, errors.Annotatef(err, "setting activation key for user %q", name)
	}
	return activationKey, nil
}

// EnableUserAuthentication will enable the user for authentication.
// The following error types are possible from this function:
// - accesserrors.UserNameNotValid: When the username supplied is not valid.
// - accesserrors.NotFound: If no user by the given UUID exists.
func (s *UserService) EnableUserAuthentication(ctx context.Context, name user.Name) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := s.st.EnableUserAuthentication(ctx, name); err != nil {
		return errors.Annotatef(err, "enabling user with uuid %q", name)
	}
	return nil
}

// DisableUserAuthentication will disable the user for authentication.
// The following error types are possible from this function:
// - accesserrors.UserNameNotValid: When the username supplied is not valid.
// - accesserrors.NotFound: If no user by the given UUID exists.
func (s *UserService) DisableUserAuthentication(ctx context.Context, name user.Name) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := s.st.DisableUserAuthentication(ctx, name); err != nil {
		return errors.Annotatef(err, "disabling user %q", name)
	}
	return nil
}

// UpdateLastModelLogin will update the last login time for the user.
// The following error types are possible from this function:
// - [accesserrors.UserNameNotValid] when the username supplied is not valid.
// - [accesserrors.UserNotFound] when the user cannot be found.
// - [modelerrors.NotFound] if no model by the given modelUUID exists.
func (s *UserService) UpdateLastModelLogin(ctx context.Context, name user.Name, modelUUID coremodel.UUID) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := s.st.UpdateLastModelLogin(ctx, name, modelUUID, time.Now()); err != nil {
		return errors.Annotatef(err, "updating last login for user %q", name)
	}
	return nil
}

// SetLastModelLogin will set the last login time for the user to the given
// value. The following error types are possible from this function:
// [accesserrors.UserNameNotValid] when the username supplied is not valid.
// [accesserrors.UserNotFound] when the user cannot be found.
// [modelerrors.NotFound] if no model by the given modelUUID exists.
func (s *UserService) SetLastModelLogin(ctx context.Context, name user.Name, modelUUID coremodel.UUID, lastLogin time.Time) error {
	if name.IsZero() {
		return errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := s.st.UpdateLastModelLogin(ctx, name, modelUUID, lastLogin); err != nil {
		return errors.Annotatef(err, "setting last login for user %q", name)
	}
	return nil
}

// LastModelLogin will return the last login time of the specified user.
// The following error types are possible from this function:
// - [accesserrors.UserNameNotValid] when the username is not valid.
// - [accesserrors.UserNotFound] when the user cannot be found.
// - [modelerrors.NotFound] if no model by the given modelUUID exists.
// - [accesserrors.UserNeverAccessedModel] if there is no record of the user
// accessing the model.
func (s *UserService) LastModelLogin(ctx context.Context, name user.Name, modelUUID coremodel.UUID) (time.Time, error) {
	if name.IsZero() {
		return time.Time{}, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if err := modelUUID.Validate(); err != nil {
		return time.Time{}, errors.Annotatef(err, "getting last model connection for %q: bad uuid", name)
	}

	lastConnection, err := s.st.LastModelLogin(ctx, name, modelUUID)
	if err != nil {
		return time.Time{}, errors.Trace(err)
	}
	return lastConnection, nil
}

// activationKeyLength is the number of bytes in an activation key.
const activationKeyLength = 32

// generateActivationKey is responsible for generating a new activation key that
// can be used for supplying to a user.
func generateActivationKey() ([]byte, error) {
	var activationKey [activationKeyLength]byte
	if _, err := rand.Read(activationKey[:]); err != nil {
		return nil, errors.Annotate(err, "generating activation key")
	}
	return activationKey[:], nil
}

// activationBoxNonceLength is the number of bytes in the nonce for the
// activation box.
const activationBoxNonceLength = 24

// Sealer is an interface that can be used to seal a byte slice.
// This will use the nonce and box for a given user to seal the payload.
type Sealer interface {
	// Seal will seal the payload using the nonce and box for the user.
	Seal(nonce, payload []byte) ([]byte, error)
}

// SetPasswordWithActivationKey will use the activation key from the user. To
// then apply the payload password. If the user does not exist an error that
// satisfies accesserrors.NotFound will be returned. If the nonce is not the
// correct length an error that satisfies errors.NotValid will be returned.
//
// This will use the NaCl secretbox to open the box and then unmarshal the
// payload to set the new password for the user. If the payload cannot be
// unmarshalled an error will be returned.
// To prevent the leaking of the key and nonce (which can unbox the secret),
// a Sealer will be returned that can be used to seal the response payload.
func (s *UserService) SetPasswordWithActivationKey(ctx context.Context, name user.Name, nonce, box []byte) (Sealer, error) {
	if name.IsZero() {
		return nil, errors.Annotatef(accesserrors.UserNameNotValid, "empty username")
	}

	if len(nonce) != activationBoxNonceLength {
		return nil, errors.NotValidf("nonce")
	}

	// Get the activation key for the user.
	key, err := s.st.GetActivationKey(ctx, name)
	if err != nil {
		return nil, errors.Trace(err)
	}

	// Copy the nonce and the key to arrays which can be used for the secretbox.
	var sbKey [activationKeyLength]byte
	var sbNonce [activationBoxNonceLength]byte
	copy(sbKey[:], key)
	copy(sbNonce[:], nonce)

	// The box is the payload that has been sealed with the nonce and key, so
	// let's open it.
	boxPayloadBytes, ok := secretbox.Open(nil, box, &sbNonce, &sbKey)
	if !ok {
		return nil, accesserrors.ActivationKeyNotValid
	}

	// We expect the payload to be a JSON object with a password field.
	var payload struct {
		// Password is the new password to set for the user.
		Password string `json:"password"`
	}
	if err := json.Unmarshal(boxPayloadBytes, &payload); err != nil {
		return nil, errors.Annotate(err, "cannot unmarshal payload")
	}

	if err := s.setPassword(ctx, name, auth.NewPassword(payload.Password)); err != nil {
		return nil, errors.Annotate(err, "setting new password")
	}

	return boxSealer{
		key: sbKey,
	}, nil
}

func (s *UserService) setPassword(ctx context.Context, name user.Name, pass auth.Password) error {
	salt, err := auth.NewSalt()
	if err != nil {
		return errors.Annotatef(err, "generating password salt for user %q", name)
	}

	pwHash, err := auth.HashPassword(pass, salt)
	if err != nil {
		return errors.Annotatef(err, "hashing password for user %q", name)
	}

	if err = s.st.SetPasswordHash(ctx, name, pwHash, salt); err != nil {
		return errors.Annotatef(err, "setting password for user %q", name)
	}

	return nil
}

// boxSealer is a Sealer that uses the NaCl secretbox to seal a payload.
type boxSealer struct {
	key [activationKeyLength]byte
}

func (s boxSealer) Seal(nonce, payload []byte) ([]byte, error) {
	if len(nonce) != activationBoxNonceLength {
		return nil, errors.NotValidf("nonce")
	}

	var sbNonce [activationBoxNonceLength]byte
	copy(sbNonce[:], nonce)
	return secretbox.Seal(nil, payload, &sbNonce, &s.key), nil
}
