# frozen_string_literal: true

module Habitat
  # The handoff. Once a project has declared its targets, `Habitat.start`
  # gives control to this loop, which fetches the plan, calls the right target
  # for each execution, times it, and posts the result back.
  #
  # None of this is application code, and none of it is grading: the loop
  # reports what happened and the engine decides what it means. That split is
  # what lets a second language's SDK be a small HTTP client rather than a
  # second implementation of habitat.
  class Runner
    def initialize(registry: Habitat.registry, client: Client.new, logger: $stderr)
      @registry = registry
      @client = client
      @logger = logger
    end

    def call
      plan = @client.plan
      executions = plan["executions"] || []
      target = @registry.fetch(plan["target"])

      log("running #{executions.length} executions against #{plan['target']}")
      executions.each { |execution| perform(execution, target) }
      @client.complete
      log("done")
    end

    private

    def perform(execution, target)
      result = invoke(target, execution)
      @client.submit(
        case_id: execution["case_id"],
        repetition_index: execution["repetition_index"],
        result:
      )
      log_outcome(execution, result)
    end

    # A target that raises fails its own execution, not the run — the other
    # cases still have something to say, and a case may even expect the error.
    def invoke(target, execution)
      started = monotonic_ms
      result = call_target(target, execution)
      result = Result.new(output: result) unless result.is_a?(Result)
      result.duration_ms ||= (monotonic_ms - started).round
      result
    rescue StandardError => e
      failed = Result.from_exception(e)
      failed.duration_ms = (monotonic_ms - started).round
      failed
    end

    def call_target(target, execution)
      input = symbolize(execution["input"] || {})
      context = { case_id: execution["case_id"], repetition_index: execution["repetition_index"] }
      target.call(input:, context:)
    end

    def symbolize(value)
      case value
      when Hash then value.to_h { |key, nested| [key.to_sym, symbolize(nested)] }
      when Array then value.map { |item| symbolize(item) }
      else value
      end
    end

    def monotonic_ms = Process.clock_gettime(Process::CLOCK_MONOTONIC, :millisecond)

    def log_outcome(execution, result)
      status = result.error ? "error" : "ok"
      log("  #{execution['case_id']} ##{execution['repetition_index']} #{status} (#{result.duration_ms}ms)")
    end

    def log(message) = @logger&.puts("[habitat] #{message}")
  end
end
