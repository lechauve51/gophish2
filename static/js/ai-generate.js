(function () {
    if (typeof window.user === "undefined" || !user.api_key || typeof query !== "function") {
        return;
    }

    var lastContent = null;

    var modalHtml = [
        '<div class="modal fade" id="aiGenerateModal" tabindex="-1" role="dialog">',
        '  <div class="modal-dialog modal-lg" role="document">',
        '    <div class="modal-content">',
        '      <div class="modal-header">',
        '        <button type="button" class="close" data-dismiss="modal"><span>&times;</span></button>',
        '        <h4 class="modal-title"><i class="fa fa-magic"></i> Generate with AI</h4>',
        '      </div>',
        '      <div class="modal-body">',
        '        <div class="row" id="aiModalFlashes"></div>',
        '        <div class="row">',
        '          <div class="col-md-6 form-group">',
        '            <label>Scenario</label>',
        '            <select class="form-control" id="aiScenario">',
        '              <option value="invoice_fraud">Invoice / payment fraud</option>',
        '              <option value="password_reset">Password reset</option>',
        '              <option value="package_delivery">Package delivery</option>',
        '              <option value="hr_policy">HR / internal policy update</option>',
        '              <option value="it_alert">IT security alert</option>',
        '              <option value="voicemail">Voicemail notification</option>',
        '              <option value="other">Other (describe below)</option>',
        '            </select>',
        '            <input type="text" class="form-control" id="aiScenarioCustom" placeholder="Describe the scenario" style="margin-top:6px; display:none;" />',
        '          </div>',
        '          <div class="col-md-3 form-group">',
        '            <label>Language</label>',
        '            <input type="text" class="form-control" id="aiLanguage" value="fr" />',
        '          </div>',
        '          <div class="col-md-3 form-group">',
        '            <label>Tone</label>',
        '            <select class="form-control" id="aiTone">',
        '              <option value="urgent">Urgent</option>',
        '              <option value="neutral" selected>Neutral</option>',
        '              <option value="friendly">Friendly</option>',
        '            </select>',
        '          </div>',
        '        </div>',
        '        <div class="row">',
        '          <div class="col-md-4 form-group"><label>Industry</label><input type="text" class="form-control" id="aiIndustry" placeholder="finance, healthcare..." /></div>',
        '          <div class="col-md-4 form-group"><label>Target audience</label><input type="text" class="form-control" id="aiAudience" placeholder="all employees" /></div>',
        '          <div class="col-md-4 form-group"><label>Impersonated company</label><input type="text" class="form-control" id="aiCompany" placeholder="Acme Corp" /></div>',
        '        </div>',
        '        <div class="row">',
        '          <div class="col-md-4 form-group"><label>Sender display name</label><input type="text" class="form-control" id="aiSender" placeholder="IT Support" /></div>',
        '          <div class="col-md-4 form-group"><label>Template name (optional)</label><input type="text" class="form-control" id="aiTemplateName" placeholder="auto" /></div>',
        '          <div class="col-md-4 form-group"><label>Page name (optional)</label><input type="text" class="form-control" id="aiPageName" placeholder="auto" /></div>',
        '        </div>',
        '        <div class="row">',
        '          <div class="col-md-4 form-group"><label>Redirect URL after submit</label><input type="text" class="form-control" id="aiRedirect" placeholder="https://intranet.example.com/training" /></div>',
        '          <div class="col-md-4 form-group" style="margin-top:24px;"><label><input type="checkbox" id="aiCapturePasswords" checked /> Capture passwords</label></div>',
        '          <div class="col-md-4 form-group"><label>Authorization reference (required to create)</label><input type="text" class="form-control" id="aiAuthorization" placeholder="ticket / change ref" /></div>',
        '        </div>',
        '        <div class="row" id="aiPreview" style="display:none;">',
        '          <div class="col-md-12"><h4>Preview</h4><div class="alert alert-info" id="aiRationale"></div></div>',
        '          <div class="col-md-6"><label>Email subject</label><p id="aiPreviewSubject"></p>',
        '            <label>Plaintext</label><pre id="aiPreviewText" style="max-height:120px; overflow:auto;"></pre>',
        '            <label>Email HTML</label><iframe id="aiPreviewEmail" sandbox="" style="width:100%; height:220px; border:1px solid #ccc;"></iframe></div>',
        '          <div class="col-md-6"><label>Landing page HTML</label><iframe id="aiPreviewLanding" sandbox="" style="width:100%; height:340px; border:1px solid #ccc;"></iframe></div>',
        '        </div>',
        '      </div>',
        '      <div class="modal-footer">',
        '        <button type="button" class="btn btn-default" data-dismiss="modal">Cancel</button>',
        '        <button type="button" class="btn btn-primary" id="aiPreviewBtn"><i class="fa fa-eye"></i> Preview</button>',
        '        <button type="button" class="btn btn-success" id="aiApplyBtn" disabled><i class="fa fa-check"></i> Create template &amp; landing page</button>',
        '      </div>',
        '    </div>',
        '  </div>',
        '</div>'
    ].join("\n");

    function aiFlash(message, ok) {
        var cls = ok ? "alert-success" : "alert-danger";
        $("#aiModalFlashes").empty().append('<div class="alert ' + cls + '">' + escapeHtml(message) + "</div>");
    }

    function collectBrief() {
        var scenario = $("#aiScenario").val();
        if (scenario === "other") {
            scenario = $("#aiScenarioCustom").val();
        }
        return {
            scenario: scenario,
            industry: $("#aiIndustry").val(),
            language: $("#aiLanguage").val(),
            tone: $("#aiTone").val(),
            target_audience: $("#aiAudience").val(),
            sender_name: $("#aiSender").val(),
            company_name: $("#aiCompany").val(),
            capture_passwords: $("#aiCapturePasswords").is(":checked"),
            redirect_url: $("#aiRedirect").val(),
            template_name: $("#aiTemplateName").val(),
            page_name: $("#aiPageName").val()
        };
    }

    function showPreview(content) {
        lastContent = content;
        $("#aiPreview").show();
        $("#aiPreviewSubject").text(content.email_subject || "");
        $("#aiPreviewText").text(content.email_text || "");
        $("#aiPreviewEmail").attr("srcdoc", content.email_html || "");
        $("#aiPreviewLanding").attr("srcdoc", content.landing_html || "");
        $("#aiRationale").text(content.rationale || "");
        $("#aiApplyBtn").prop("disabled", false);
    }

    function preview() {
        aiFlash("Generating...", true);
        $("#aiPreview").hide();
        $("#aiApplyBtn").prop("disabled", true);
        query("/ai/generate", "POST", collectBrief()).done(function (resp) {
            if (resp && resp.success && resp.data) {
                showPreview(resp.data.content);
                if (resp.data.template_name && !$("#aiTemplateName").val()) {
                    $("#aiTemplateName").val(resp.data.template_name);
                }
                if (resp.data.page_name && !$("#aiPageName").val()) {
                    $("#aiPageName").val(resp.data.page_name);
                }
                aiFlash("Preview ready. Review the content, then create.", true);
            } else {
                aiFlash((resp && resp.message) || "Generation failed", false);
            }
        }).fail(function (xhr) {
            var msg = "Generation failed";
            try {
                msg = JSON.parse(xhr.responseText).message || msg;
            } catch (e) {}
            aiFlash(msg, false);
        });
    }

    function apply() {
        if (!lastContent) {
            aiFlash("Preview first.", false);
            return;
        }
        var authorization = $("#aiAuthorization").val().trim();
        if (!authorization) {
            aiFlash("An authorization reference is required to create AI-generated content.", false);
            return;
        }
        var brief = collectBrief();
        brief.apply = true;
        brief.authorization = authorization;
        brief.content = lastContent;
        query("/ai/campaign", "POST", brief).done(function (resp) {
            if (resp && resp.success && resp.data) {
                aiFlash("Created: template #" + (resp.data.template_id || "?") + ", page #" + (resp.data.page_id || "?"), true);
                setTimeout(function () {
                    $("#aiGenerateModal").modal("hide");
                    location.reload();
                }, 1200);
            } else {
                aiFlash((resp && resp.message) || "Creation failed", false);
            }
        }).fail(function (xhr) {
            var msg = "Creation failed";
            try {
                msg = JSON.parse(xhr.responseText).message || msg;
            } catch (e) {}
            aiFlash(msg, false);
        });
    }

    $(document).ready(function () {
        var btn = $("#aiGenerateBtn");
        if (btn.length === 0) {
            return;
        }
        $("body").append(modalHtml);
        btn.on("click", function () {
            lastContent = null;
            $("#aiPreview").hide();
            $("#aiApplyBtn").prop("disabled", true);
            $("#aiModalFlashes").empty();
            $("#aiGenerateModal").modal("show");
        });
        $("#aiScenario").on("change", function () {
            $("#aiScenarioCustom").toggle($("#aiScenario").val() === "other");
        });
        $("#aiPreviewBtn").on("click", preview);
        $("#aiApplyBtn").on("click", apply);
    });
})();
