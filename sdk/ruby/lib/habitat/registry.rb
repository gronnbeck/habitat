# frozen_string_literal: true

module Habitat
  # Where a project declares its agents.
  #
  # A target is just "a name, and how to run it". Registering one is the whole
  # of the SDK's authoring surface — everything after that (fetching the plan,
  # iterating, timing, posting results back) is handed off to the Runner.
  class Registry
    class UnknownTarget < Habitat::Error; end

    def initialize
      @targets = {}
    end

    # Registers a target, either as a block or as anything responding to
    # #call(input:, context:).
    #
    #   Habitat.target("coach_chat") { |input:, context:| ... }
    #   Habitat.target("coach_chat", CoachTarget.new)
    def register(name, callable = nil, &block)
      target = callable || block
      raise ArgumentError, "target #{name} needs a callable or a block" if target.nil?

      @targets[name.to_s] = target
    end

    def fetch(name)
      @targets.fetch(name.to_s) do
        raise UnknownTarget,
              "no target registered as #{name.inspect} (registered: #{names.join(', ')})"
      end
    end

    def registered?(name) = @targets.key?(name.to_s)

    def names = @targets.keys.sort
  end
end
