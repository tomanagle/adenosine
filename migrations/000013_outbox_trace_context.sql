ALTER TABLE ops.outbox_events
    ADD COLUMN traceparent TEXT,
    ADD COLUMN tracestate TEXT,
    ADD CONSTRAINT outbox_traceparent_length CHECK (traceparent IS NULL OR length(traceparent) <= 55),
    ADD CONSTRAINT outbox_tracestate_length CHECK (tracestate IS NULL OR length(tracestate) <= 512);
