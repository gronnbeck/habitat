# frozen_string_literal: true

require_relative "lib/habitat/version"

Gem::Specification.new do |spec|
  spec.name = "habitat-sdk"
  spec.version = Habitat::VERSION
  spec.authors = ["Ken Grønnbeck"]

  spec.summary = "Ruby SDK for habitat, an evaluation engine for non-deterministic code"
  spec.description = <<~TEXT
    Declare the agents you want evaluated and hand off to habitat's runner.
    The engine parses suites, grades results and applies policy; this SDK only
    registers targets and reports what happened.
  TEXT
  spec.homepage = "https://github.com/gronnbeck/habitat"
  spec.license = "MIT"
  spec.required_ruby_version = ">= 3.1"

  spec.metadata["homepage_uri"] = spec.homepage
  spec.metadata["source_code_uri"] = spec.homepage
  spec.metadata["rubygems_mfa_required"] = "true"

  spec.files = Dir["lib/**/*.rb", "README.md"]
  spec.require_paths = ["lib"]
end
