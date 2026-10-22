package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type showcaseRuntimeSettingRepo struct {
	SettingRepository
	values   map[string]string
	readErr  error
	writeErr error
	saved    map[string]string
}

func (r *showcaseRuntimeSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, exists := r.values[key]; exists {
			values[key] = value
		}
	}
	return values, nil
}

func (r *showcaseRuntimeSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.saved = values
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestPelicanShowcaseRuntimeAPISwitchDefaultsAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  map[string]string
		enabled bool
	}{
		{name: "unset compatibility", values: map[string]string{}, enabled: true},
		{name: "enabled", values: map[string]string{SettingKeyPelicanShowcaseAPIEnabled: "true"}, enabled: true},
		{name: "disabled", values: map[string]string{SettingKeyPelicanShowcaseAPIEnabled: "false"}},
		{name: "empty configured value", values: map[string]string{SettingKeyPelicanShowcaseAPIEnabled: ""}},
		{name: "invalid configured value", values: map[string]string{SettingKeyPelicanShowcaseAPIEnabled: "broken"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.values[SettingKeyPelicanShowcaseEnabled] = "true"
			svc := &SettingService{settingRepo: &showcaseRuntimeSettingRepo{values: tc.values}}
			runtime, err := svc.GetPelicanShowcaseRuntime(context.Background())
			require.NoError(t, err)
			require.True(t, runtime.Enabled, "the independent API switch does not disable the gallery")
			require.Equal(t, tc.enabled, runtime.APIEnabled)
		})
	}
	for _, repo := range []*showcaseRuntimeSettingRepo{
		{readErr: errors.New("settings unavailable")},
		{values: map[string]string{SettingKeyPelicanShowcaseConfig: "{broken"}},
	} {
		runtime, err := (&SettingService{settingRepo: repo}).GetPelicanShowcaseRuntime(context.Background())
		require.Error(t, err)
		require.False(t, runtime.APIEnabled)
	}
}

func TestUpdatePelicanShowcaseAPISwitchPreservesOmittedSetting(t *testing.T) {
	for _, initial := range []map[string]string{
		{}, {SettingKeyPelicanShowcaseAPIEnabled: "false"}, {SettingKeyPelicanShowcaseAPIEnabled: "true"},
	} {
		repo := &showcaseRuntimeSettingRepo{values: initial}
		svc := &SettingService{settingRepo: repo}
		before := pelicanShowcaseAPIEnabled(initial)
		runtime, err := svc.UpdatePelicanShowcaseSettings(context.Background(), true, PelicanShowcaseConfig{}, nil)
		require.NoError(t, err)
		require.Equal(t, before, runtime.APIEnabled)
		require.NotContains(t, repo.saved, SettingKeyPelicanShowcaseAPIEnabled)
		for _, enabled := range []bool{false, true} {
			runtime, err = svc.UpdatePelicanShowcaseSettings(context.Background(), true, PelicanShowcaseConfig{}, &enabled)
			require.NoError(t, err)
			require.Equal(t, enabled, runtime.APIEnabled)
			require.Equal(t, enabled, pelicanShowcaseAPIEnabled(repo.values))
		}
	}
}

func TestUpdatePelicanShowcaseOmittedAPISwitchReadFailureDoesNotWrite(t *testing.T) {
	repo := &showcaseRuntimeSettingRepo{readErr: errors.New("settings unavailable")}
	_, err := (&SettingService{settingRepo: repo}).UpdatePelicanShowcaseSettings(context.Background(), true, PelicanShowcaseConfig{}, nil)
	require.ErrorIs(t, err, repo.readErr)
	require.Nil(t, repo.saved)
}

func TestUpdatePelicanShowcaseGroupOrder(t *testing.T) {
	repo := &showcaseRuntimeSettingRepo{values: map[string]string{}}
	svc := &SettingService{settingRepo: repo}
	ctx := context.Background()
	_, err := svc.UpdatePelicanShowcaseSettings(ctx, true, PelicanShowcaseConfig{GroupOrder: []int64{9, 3}}, nil)
	require.NoError(t, err)
	runtime, err := svc.GetPelicanShowcaseRuntime(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{9, 3}, runtime.Config.GroupOrder)

	// Existing clients can change limits without erasing the order.
	runtime, err = svc.UpdatePelicanShowcaseSettings(ctx, true, PelicanShowcaseConfig{MaxItems: 30}, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{9, 3}, runtime.Config.GroupOrder)
	saved := repo.values[SettingKeyPelicanShowcaseConfig]
	for _, invalid := range [][]int64{{3, 3}, {0}, {-1}} {
		_, err := svc.UpdatePelicanShowcaseSettings(ctx, true, PelicanShowcaseConfig{GroupOrder: invalid}, nil)
		require.Error(t, err)
		require.Equal(t, saved, repo.values[SettingKeyPelicanShowcaseConfig])
	}
	_, err = svc.UpdatePelicanShowcaseSettings(ctx, true, PelicanShowcaseConfig{GroupOrder: []int64{}}, nil)
	require.NoError(t, err)
	runtime, err = svc.GetPelicanShowcaseRuntime(ctx)
	require.NoError(t, err)
	require.Empty(t, runtime.Config.GroupOrder)
}
