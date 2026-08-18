# frozen_string_literal: true

module Habitat
  # What a target reports for one execution.
  #
  # A target never decides whether it passed — it only normalises what
  # happened. Grading is the engine's job, which is why there is no assertion
  # of any kind on this object.
  #
  #   Habitat::Result.new(
  #     output: response.text,
  #     final_state: { proposed: true, action_count: 3 },
  #     usage: Habitat::Usage.new(cost_usd: 0.012)
  #   )
  #
  # `final_state` is what state_match reads, so put the structured fields you
  # intend to grade there rather than relying on prose in `output` — wording
  # varies between otherwise-correct runs, structured fields do not.
  class Result
    attr_reader :output, :final_state, :events, :usage, :error
    attr_accessor :duration_ms

    def initialize(output: nil, final_state: {}, events: [], usage: nil, duration_ms: nil, error: nil)
      @output = output
      @final_state = final_state || {}
      @events = events || []
      @usage = usage
      @duration_ms = duration_ms
      @error = error
    end

    # Builds the Result for a target that raised. The engine still grades it:
    # a case may legitimately expect an error, so this is not a verdict.
    def self.from_exception(exception)
      new(error: {
        type: "exception",
        class: exception.class.name,
        message: exception.message
      })
    end

    def to_h
      {
        output: output,
        final_state: final_state,
        events: events,
        usage: usage.respond_to?(:to_h) ? usage&.to_h : usage,
        duration_ms: duration_ms.to_i,
        error: error
      }
    end
  end
end
