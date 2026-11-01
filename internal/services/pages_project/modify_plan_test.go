package pages_project

import (
	"context"
	"testing"

	"github.com/cloudflare/terraform-provider-cloudflare/internal/customfield"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// planTestProvider serves only this resource, so a plan runs through the framework
// exactly as Terraform drives it: defaults, unknown marking, then ModifyPlan.
type planTestProvider struct{}

func (planTestProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudflare"
}

func (planTestProvider) Schema(context.Context, provider.SchemaRequest, *provider.SchemaResponse) {}

func (planTestProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}

func (planTestProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewResource}
}

func (planTestProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// objectWith returns an object of typ with every attribute null except those in set.
func objectWith(typ tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	vals := make(map[string]tftypes.Value, len(typ.AttributeTypes))
	for name, attrType := range typ.AttributeTypes {
		vals[name] = tftypes.NewValue(attrType, nil)
	}
	for name, v := range set {
		vals[name] = v
	}
	return tftypes.NewValue(typ, vals)
}

func projectType(ctx context.Context) tftypes.Object {
	return ResourceSchema(ctx).Type().TerraformType(ctx).(tftypes.Object)
}

// requiredConfig is a config that sets only the required attributes, plus overrides.
func requiredConfig(ctx context.Context, set map[string]tftypes.Value) tftypes.Value {
	vals := map[string]tftypes.Value{
		"account_id":        tftypes.NewValue(tftypes.String, "account"),
		"name":              tftypes.NewValue(tftypes.String, "example"),
		"production_branch": tftypes.NewValue(tftypes.String, "main"),
	}
	for name, v := range set {
		vals[name] = v
	}
	return objectWith(projectType(ctx), vals)
}

// deploymentConfigs builds the stored deployment_configs of a Direct Upload project.
func deploymentConfigs(ctx context.Context, failOpen bool, buildImageVersion int64) tftypes.Value {
	dcType := projectType(ctx).AttributeTypes["deployment_configs"].(tftypes.Object)

	env := func(name string) tftypes.Value {
		return objectWith(dcType.AttributeTypes[name].(tftypes.Object), map[string]tftypes.Value{
			"always_use_latest_compatibility_date": tftypes.NewValue(tftypes.Bool, false),
			"build_image_major_version":            tftypes.NewValue(tftypes.Number, buildImageVersion),
			"compatibility_date":                   tftypes.NewValue(tftypes.String, "2026-07-03"),
			"fail_open":                            tftypes.NewValue(tftypes.Bool, failOpen),
			"usage_model":                          tftypes.NewValue(tftypes.String, "standard"),
		})
	}

	return objectWith(dcType, map[string]tftypes.Value{"preview": env("preview"), "production": env("production")})
}

// storedProject is the refreshed state of a deployed project, with every computed
// attribute set as the API returns it, plus overrides.
func storedProject(ctx context.Context, set map[string]tftypes.Value) tftypes.Value {
	typ := projectType(ctx)
	attr := func(name string) tftypes.Object { return typ.AttributeTypes[name].(tftypes.Object) }

	vals := map[string]tftypes.Value{
		"id":                       tftypes.NewValue(tftypes.String, "example"),
		"account_id":               tftypes.NewValue(tftypes.String, "account"),
		"name":                     tftypes.NewValue(tftypes.String, "example"),
		"production_branch":        tftypes.NewValue(tftypes.String, "main"),
		"build_config":             objectWith(attr("build_config"), map[string]tftypes.Value{"destination_dir": tftypes.NewValue(tftypes.String, "dist")}),
		"deployment_configs":       deploymentConfigs(ctx, true, 3),
		"created_on":               tftypes.NewValue(tftypes.String, "2026-07-03T09:52:11Z"),
		"framework":                tftypes.NewValue(tftypes.String, ""),
		"framework_version":        tftypes.NewValue(tftypes.String, ""),
		"preview_script_name":      tftypes.NewValue(tftypes.String, "pages-worker--1-preview"),
		"production_script_name":   tftypes.NewValue(tftypes.String, "pages-worker--1-production"),
		"subdomain":                tftypes.NewValue(tftypes.String, "example.pages.dev"),
		"uses_functions":           tftypes.NewValue(tftypes.Bool, false),
		"domains":                  tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "example.pages.dev")}),
		"canonical_deployment":     objectWith(attr("canonical_deployment"), map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "deployment")}),
		"latest_deployment":        objectWith(attr("latest_deployment"), map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "deployment")}),
		"build_image_auto_upgrade": tftypes.NewValue(attr("build_image_auto_upgrade"), nil),
	}
	for name, v := range set {
		vals[name] = v
	}
	return objectWith(typ, vals)
}

func autoUpgrade(ctx context.Context, from, to int64) tftypes.Value {
	return tftypes.NewValue(projectType(ctx).AttributeTypes["build_image_auto_upgrade"].(tftypes.Object), map[string]tftypes.Value{
		"from_version": tftypes.NewValue(tftypes.Number, from),
		"to_version":   tftypes.NewValue(tftypes.Number, to),
	})
}

// planProject runs PlanResourceChange through the provider server and returns the planned state.
func planProject(t *testing.T, ctx context.Context, config, prior, proposed tftypes.Value) tftypes.Value {
	t.Helper()

	server, err := providerserver.NewProtocol6WithError(planTestProvider{})()
	if err != nil {
		t.Fatalf("creating provider server: %v", err)
	}

	dynamic := func(v tftypes.Value) *tfprotov6.DynamicValue {
		dv, err := tfprotov6.NewDynamicValue(projectType(ctx), v)
		if err != nil {
			t.Fatalf("encoding value: %v", err)
		}
		return &dv
	}

	resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "cloudflare_pages_project",
		Config:           dynamic(config),
		PriorState:       dynamic(prior),
		ProposedNewState: dynamic(proposed),
	})
	if err != nil {
		t.Fatalf("PlanResourceChange: %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("PlanResourceChange diagnostic: %s: %s", d.Summary, d.Detail)
		}
	}

	planned, err := resp.PlannedState.Unmarshal(projectType(ctx))
	if err != nil {
		t.Fatalf("decoding planned state: %v", err)
	}
	return planned
}

func assertPlanEqualsState(t *testing.T, planned, prior tftypes.Value) {
	t.Helper()

	diffs, err := prior.Diff(planned)
	if err != nil {
		t.Fatalf("diffing plan against state: %v", err)
	}
	for _, d := range diffs {
		t.Errorf("planned state differs at %s: state %v, plan %v", d.Path, d.Value1, d.Value2)
	}
}

func plannedAutoUpgrade(t *testing.T, planned tftypes.Value) tftypes.Value {
	t.Helper()

	raw, _, err := tftypes.WalkAttributePath(planned, tftypes.NewAttributePath().WithAttributeName("build_image_auto_upgrade"))
	if err != nil {
		t.Fatalf("reading build_image_auto_upgrade: %v", err)
	}
	return raw.(tftypes.Value)
}

// TestModifyPlan_NoDiffWhenDefaultRewritesOmittedDeploymentConfigs reproduces the
// reported drift: config omits deployment_configs while state holds fail_open = false.
// The schema default rewrites fail_open in the proposal, the framework then marks
// computed attributes unknown, and the plan must still come back equal to state.
func TestModifyPlan_NoDiffWhenDefaultRewritesOmittedDeploymentConfigs(t *testing.T) {
	ctx := context.Background()

	null := func(name string) tftypes.Value { return tftypes.NewValue(projectType(ctx).AttributeTypes[name], nil) }

	for name, set := range map[string]map[string]tftypes.Value{
		"deployed project": {},
		"build image upgraded by Cloudflare": {
			"build_image_auto_upgrade": autoUpgrade(ctx, 1, 3),
		},
		"project without deployments": {
			"build_config":         null("build_config"),
			"canonical_deployment": null("canonical_deployment"),
			"latest_deployment":    null("latest_deployment"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			set["deployment_configs"] = deploymentConfigs(ctx, false, 3)
			prior := storedProject(ctx, set)

			planned := planProject(t, ctx, requiredConfig(ctx, nil), prior, prior)

			assertPlanEqualsState(t, planned, prior)
		})
	}
}

// TestModifyPlan_KeepsAutoUpgradeUnknownWhenBuildImageVersionChanges covers an
// update the API answers with a new build_image_auto_upgrade, so the plan must not
// promise the value in state.
func TestModifyPlan_KeepsAutoUpgradeUnknownWhenBuildImageVersionChanges(t *testing.T) {
	ctx := context.Background()

	dcType := projectType(ctx).AttributeTypes["deployment_configs"].(tftypes.Object)
	envVersion := func(env string, version int64) tftypes.Value {
		return objectWith(dcType.AttributeTypes[env].(tftypes.Object), map[string]tftypes.Value{
			"build_image_major_version": tftypes.NewValue(tftypes.Number, version),
		})
	}
	config := requiredConfig(ctx, map[string]tftypes.Value{
		"deployment_configs": objectWith(dcType, map[string]tftypes.Value{
			"preview":    envVersion("preview", 2),
			"production": envVersion("production", 2),
		}),
	})

	prior := storedProject(ctx, map[string]tftypes.Value{"build_image_auto_upgrade": autoUpgrade(ctx, 1, 3)})
	// Terraform core proposes the configured version and keeps state for the computed rest.
	proposed := storedProject(ctx, map[string]tftypes.Value{
		"deployment_configs":       deploymentConfigs(ctx, true, 2),
		"build_image_auto_upgrade": autoUpgrade(ctx, 1, 3),
	})

	planned := planProject(t, ctx, config, prior, proposed)

	if got := plannedAutoUpgrade(t, planned); got.IsKnown() {
		t.Errorf("build_image_auto_upgrade = %v, want unknown while the build image version changes", got)
	}
}

// TestModifyPlan_NoDiffWhenEnvVarsSetOutsideConfig covers env vars added in the
// dashboard: env_vars is Optional only, so Terraform core proposes a null
// deployment_configs, and the plan must still come back equal to state.
func TestModifyPlan_NoDiffWhenEnvVarsSetOutsideConfig(t *testing.T) {
	ctx := context.Background()

	dcType := projectType(ctx).AttributeTypes["deployment_configs"].(tftypes.Object)
	withEnvVar := func(env string) tftypes.Value {
		envType := dcType.AttributeTypes[env].(tftypes.Object)
		varsType := envType.AttributeTypes["env_vars"].(tftypes.Map)
		varType := varsType.ElementType.(tftypes.Object)

		stored, _, err := tftypes.WalkAttributePath(deploymentConfigs(ctx, true, 3), tftypes.NewAttributePath().WithAttributeName(env))
		if err != nil {
			t.Fatalf("reading %s: %v", env, err)
		}
		vals := map[string]tftypes.Value{}
		if err := stored.(tftypes.Value).As(&vals); err != nil {
			t.Fatalf("decoding %s: %v", env, err)
		}
		vals["env_vars"] = tftypes.NewValue(varsType, map[string]tftypes.Value{
			"SKIP_DEPENDENCY_INSTALL": objectWith(varType, map[string]tftypes.Value{
				"type":  tftypes.NewValue(tftypes.String, "plain_text"),
				"value": tftypes.NewValue(tftypes.String, "true"),
			}),
		})
		return tftypes.NewValue(envType, vals)
	}

	prior := storedProject(ctx, map[string]tftypes.Value{
		"deployment_configs": tftypes.NewValue(dcType, map[string]tftypes.Value{
			"preview":    withEnvVar("preview"),
			"production": withEnvVar("production"),
		}),
	})
	proposed := storedProject(ctx, map[string]tftypes.Value{
		"deployment_configs": tftypes.NewValue(dcType, nil),
	})

	planned := planProject(t, ctx, requiredConfig(ctx, nil), prior, proposed)

	assertPlanEqualsState(t, planned, prior)
}

// TestModifyPlan_KeepsBuildConfigUnknownWhenConfigIsUnknown covers build_config taken
// from another resource: its value is unknown in config, so the plan must not promise
// the null in state.
func TestModifyPlan_KeepsBuildConfigUnknownWhenConfigIsUnknown(t *testing.T) {
	ctx := context.Background()

	bcType := projectType(ctx).AttributeTypes["build_config"]
	unknown := tftypes.NewValue(bcType, tftypes.UnknownValue)
	set := map[string]tftypes.Value{"build_config": tftypes.NewValue(bcType, nil)}

	prior := storedProject(ctx, set)
	set["build_config"] = unknown
	proposed := storedProject(ctx, set)

	planned := planProject(t, ctx, requiredConfig(ctx, map[string]tftypes.Value{"build_config": unknown}), prior, proposed)

	raw, _, err := tftypes.WalkAttributePath(planned, tftypes.NewAttributePath().WithAttributeName("build_config"))
	if err != nil {
		t.Fatalf("reading build_config: %v", err)
	}
	if got := raw.(tftypes.Value); got.IsKnown() {
		t.Errorf("build_config = %v, want unknown while its config value is unknown", got)
	}
}

func TestBuildImageVersionsMatch(t *testing.T) {
	ctx := context.Background()

	dcType := projectType(ctx).AttributeTypes["deployment_configs"].(tftypes.Object)
	version := func(v any) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
	configs := func(preview, production tftypes.Value) tftypes.Value {
		env := func(name string, v tftypes.Value) tftypes.Value {
			return objectWith(dcType.AttributeTypes[name].(tftypes.Object), map[string]tftypes.Value{"build_image_major_version": v})
		}
		return tftypes.NewValue(dcType, map[string]tftypes.Value{"preview": env("preview", preview), "production": env("production", production)})
	}
	// toModel reads a raw deployment_configs value the way ModifyPlan receives it.
	toModel := func(v tftypes.Value) customfield.NestedObject[PagesProjectDeploymentConfigsModel] {
		var m PagesProjectModel
		state := tfsdk.State{Schema: ResourceSchema(ctx), Raw: storedProject(ctx, map[string]tftypes.Value{"deployment_configs": v})}
		if diags := state.Get(ctx, &m); diags.HasError() {
			t.Fatalf("decoding deployment_configs: %v", diags)
		}
		return m.DeploymentConfigs
	}

	same := configs(version(3), version(3))
	null := tftypes.NewValue(dcType, nil)

	for name, tc := range map[string]struct {
		plan, state tftypes.Value
		want        bool
	}{
		"same versions":                   {same, same, true},
		"production version changes":      {configs(version(3), version(2)), same, false},
		"preview version changes":         {configs(version(2), version(3)), same, false},
		"planned version unknown":         {configs(version(3), version(tftypes.UnknownValue)), same, false},
		"deployment_configs unknown":      {tftypes.NewValue(dcType, tftypes.UnknownValue), same, false},
		"both deployment_configs null":    {null, null, true},
		"deployment_configs null in plan": {null, same, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := buildImageVersionsMatch(ctx, toModel(tc.plan), toModel(tc.state)); got != tc.want {
				t.Errorf("buildImageVersionsMatch = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKeepPlannedReadOnlyValues(t *testing.T) {
	ctx := context.Background()

	response := func() *PagesProjectModel {
		return &PagesProjectModel{
			CanonicalDeployment:   customfield.NewObjectMust(ctx, &PagesProjectCanonicalDeploymentModel{ID: types.StringValue("newer")}),
			LatestDeployment:      customfield.NewObjectMust(ctx, &PagesProjectLatestDeploymentModel{ID: types.StringValue("newer")}),
			BuildImageAutoUpgrade: customfield.NewObjectMust(ctx, &PagesProjectBuildImageAutoUpgradeModel{FromVersion: types.Int64Value(1), ToVersion: types.Int64Value(3)}),
		}
	}

	t.Run("known plan values win over a newer response", func(t *testing.T) {
		planned := &PagesProjectModel{
			CanonicalDeployment:   customfield.NullObject[PagesProjectCanonicalDeploymentModel](ctx),
			LatestDeployment:      customfield.NewObjectMust(ctx, &PagesProjectLatestDeploymentModel{ID: types.StringValue("planned")}),
			BuildImageAutoUpgrade: customfield.NullObject[PagesProjectBuildImageAutoUpgradeModel](ctx),
		}
		data := response()

		keepPlannedReadOnlyValues(planned, data)

		if !data.CanonicalDeployment.Equal(planned.CanonicalDeployment) {
			t.Errorf("canonical_deployment = %v, want planned %v", data.CanonicalDeployment, planned.CanonicalDeployment)
		}
		if !data.LatestDeployment.Equal(planned.LatestDeployment) {
			t.Errorf("latest_deployment = %v, want planned %v", data.LatestDeployment, planned.LatestDeployment)
		}
		if !data.BuildImageAutoUpgrade.Equal(planned.BuildImageAutoUpgrade) {
			t.Errorf("build_image_auto_upgrade = %v, want planned %v", data.BuildImageAutoUpgrade, planned.BuildImageAutoUpgrade)
		}
	})

	t.Run("unknown plan values take the response", func(t *testing.T) {
		planned := &PagesProjectModel{
			CanonicalDeployment:   customfield.UnknownObject[PagesProjectCanonicalDeploymentModel](ctx),
			LatestDeployment:      customfield.UnknownObject[PagesProjectLatestDeploymentModel](ctx),
			BuildImageAutoUpgrade: customfield.UnknownObject[PagesProjectBuildImageAutoUpgradeModel](ctx),
		}
		data, want := response(), response()

		keepPlannedReadOnlyValues(planned, data)

		if !data.CanonicalDeployment.Equal(want.CanonicalDeployment) || !data.LatestDeployment.Equal(want.LatestDeployment) || !data.BuildImageAutoUpgrade.Equal(want.BuildImageAutoUpgrade) {
			t.Errorf("unknown plan values replaced the response: %+v", data)
		}
	})
}
