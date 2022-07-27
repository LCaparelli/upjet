/*
Copyright 2022 Upbound Inc.
*/

package reference

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/config"
)

const (
	extractorPackagePath      = "github.com/upbound/upjet/pkg/resource"
	extractResourceIDFuncPath = extractorPackagePath + ".ExtractResourceID()"
	fmtExtractParamFuncPath   = extractorPackagePath + `.ExtractParamPath("%s")`
)

// Injector resolves references using provider metadata
type Injector struct {
	ModulePath        string
	ProviderShortName string
}

// NewInjector initializes a new Injector
func NewInjector(modulePath string) *Injector {
	return &Injector{
		ModulePath: modulePath,
	}
}

func getExtractorFuncPath(sourceAttr string) string {
	switch sourceAttr {
	// value extractor from status.atProvider.id
	case "id":
		return extractResourceIDFuncPath
	// value extractor from spec.forProvider.<attr>
	default:
		return fmt.Sprintf(fmtExtractParamFuncPath, sourceAttr)
	}
}

// InjectReferences injects cross-resource references using the
// provider metadata scraped from the Terraform registry.
func (rr *Injector) InjectReferences(configResources map[string]*config.Resource) error { // nolint:gocyclo
	for n, r := range configResources {
		m := configResources[n].MetaResource
		if m == nil {
			continue
		}

		for _, re := range m.Examples {
			pm, err := paveExampleManifest(re.Manifest)
			if err != nil {
				return errors.Wrapf(err, "cannot pave example manifest for resource: %s", n)
			}
			resolutionContext, err := PrepareLocalResolutionContext(re)
			if err != nil {
				return errors.Wrapf(err, "cannot prepare local resolution context for resource: %s", n)
			}
			if err := rr.ResolveReferencesOfPaved(pm, resolutionContext); err != nil {
				return errors.Wrapf(err, "cannot resolve references of resource with local examples context: %s", n)
			}
			for targetAttr, ref := range re.References {
				// if a reference is already configured for the target attribute
				if _, ok := r.References[targetAttr]; ok {
					continue
				}
				parts := getRefParts(ref)
				if parts == nil {
					continue
				}
				if skipReference(configResources[n].SkipReferencesTo, parts) {
					continue
				}
				if _, ok := configResources[parts.Resource]; !ok {
					continue
				}
				r.References[targetAttr] = config.Reference{
					TerraformName: parts.Resource,
					Extractor:     getExtractorFuncPath(parts.Attribute),
				}
			}
		}
	}
	return nil
}

func skipReference(skippedRefs []string, parts *Parts) bool {
	for _, p := range skippedRefs {
		if p == parts.getResourceAttr() {
			return true
		}
	}
	return false
}

func (rr *Injector) getTypePath(tfName string, configResources map[string]*config.Resource) (string, error) {
	r := configResources[tfName]
	if r == nil {
		return "", errors.Errorf("cannot find configuration for Terraform resource: %s", tfName)
	}
	shortGroup := r.ShortGroup
	if len(shortGroup) == 0 {
		shortGroup = rr.ProviderShortName
	}
	return fmt.Sprintf("%s/%s/%s/%s.%s", rr.ModulePath, "apis", shortGroup, r.Version, r.Kind), nil
}

// SetReferenceTypes resolves reference types of configured references
// using their TerraformNames.
func (rr *Injector) SetReferenceTypes(configResources map[string]*config.Resource) error {
	for _, r := range configResources {
		for attr, ref := range r.References {
			if ref.Type == "" && ref.TerraformName != "" {
				crdTypePath, err := rr.getTypePath(ref.TerraformName, configResources)
				if err != nil {
					return errors.Wrap(err, "cannot set reference types")
				}
				// TODO(aru): if type mapper cannot provide a mapping,
				// currently we remove the reference. Once,
				// we have type mapper implementations available
				// for all providers, then we can keep the refs
				// instead of removing them, and expect resulting
				// compile errors to be fixed by making the types
				// available to the type mapper.
				if crdTypePath == "" {
					delete(r.References, attr)
					continue
				}
				ref.Type = crdTypePath
				r.References[attr] = ref
			}
		}
	}
	return nil
}