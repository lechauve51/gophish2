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

(function () {
    if (typeof window.user === "undefined" || !user.api_key || typeof query !== "function") {
        return;
    }

    var chatModalHtml = [
        '<div class="modal fade" id="aiChatModal" tabindex="-1" role="dialog">',
        '  <div class="modal-dialog modal-lg" role="document">',
        '    <div class="modal-content">',
        '      <div class="modal-header">',
        '        <button type="button" class="close" data-dismiss="modal"><span>&times;</span></button>',
        '        <h4 class="modal-title"><i class="fa fa-comments"></i> AI Assistant</h4>',
        '      </div>',
        '      <div class="modal-body">',
        '        <div id="aiChatMessages" style="height:420px; overflow-y:auto; padding:8px; background:#f7f7f7; border:1px solid #ddd; border-radius:4px;"></div>',
        '      </div>',
        '      <div class="modal-footer">',
        '        <div class="input-group">',
        '          <input type="text" class="form-control" id="aiChatInput" placeholder="Décrivez ce que vous voulez configurer (domaine, SMTP, cibles, scénario...)" />',
        '          <span class="input-group-btn">',
        '            <button class="btn btn-primary" id="aiChatSend"><i class="fa fa-paper-plane"></i> Envoyer</button>',
        '          </span>',
        '        </div>',
        '        <p class="help-block" style="margin-top:6px;">L\'assistant peut lire et configurer toute l\'application (templates, landing pages, groupes, profils SMTP, campagnes). La création de contenu ou le lancement d\'une campagne exige une référence d\'autorisation.</p>',
        '      </div>',
        '    </div>',
        '  </div>',
        '</div>'
    ].join("\n");

    function chatBubble(role, text, toolCalls) {
        var cls = role === "user" ? "primary" : "default";
        var align = role === "user" ? "text-align:right;" : "text-align:left;";
        var html = '<div style="' + align + ' margin:6px 0;"><span class="label label-' + cls + '" style="font-size:13px; padding:6px 10px; border-radius:8px; display:inline-block; max-width:85%; white-space:pre-wrap; text-align:left;">' + escapeHtml(text || "") + "</span>";
        if (toolCalls && toolCalls.length) {
            var names = toolCalls.map(function (tc) { return tc.function.name; }).join(", ");
            html += '<div style="font-size:11px; color:#888;">' + escapeHtml("outils: " + names) + "</div>";
        }
        html += "</div>";
        return html;
    }

    function renderChatMessages(messages) {
        var box = $("#aiChatMessages").empty();
        (messages || []).forEach(function (m) {
            if (m.role === "user" || m.role === "assistant") {
                box.append(chatBubble(m.role, m.content, m.tool_calls));
            }
        });
        box.scrollTop(box.prop("scrollHeight"));
    }

    function chatError(text) {
        $("#aiChatMessages").append(chatBubble("assistant", "Erreur: " + text));
        $("#aiChatMessages").scrollTop($("#aiChatMessages").prop("scrollHeight"));
    }

    function sendChatMessage() {
        var input = $("#aiChatInput");
        var text = input.val().trim();
        if (!text) {
            return;
        }
        input.val("");
        $("#aiChatSend").prop("disabled", true);
        var payload = { message: text };
        var cid = localStorage.getItem("gophish_ai_conv");
        if (cid) {
            payload.conversation_id = cid;
        }
        $("#aiChatMessages").append(chatBubble("user", text));
        $("#aiChatMessages").scrollTop($("#aiChatMessages").prop("scrollHeight"));
        query("/ai/chat", "POST", payload).done(function (resp) {
            if (resp && resp.success && resp.data) {
                localStorage.setItem("gophish_ai_conv", resp.data.conversation_id);
                renderChatMessages(resp.data.messages);
            } else {
                chatError((resp && resp.message) || "échec de la requête");
            }
        }).fail(function (xhr) {
            var msg = "requête échouée";
            try {
                msg = JSON.parse(xhr.responseText).message || msg;
            } catch (e) {}
            chatError(msg);
        }).always(function () {
            $("#aiChatSend").prop("disabled", false);
            input.focus();
        });
    }

    $(document).ready(function () {
        var openBtn = $("#aiChatOpen");
        if (openBtn.length === 0) {
            return;
        }
        $("body").append(chatModalHtml);
        openBtn.on("click", function (e) {
            e.preventDefault();
            $("#aiChatModal").modal("show");
            var cid = localStorage.getItem("gophish_ai_conv");
            if (cid) {
                query("/ai/chat?conversation_id=" + encodeURIComponent(cid), "GET", null).done(function (resp) {
                    if (resp && resp.success && resp.data) {
                        renderChatMessages(resp.data.messages);
                        return;
                    }
                    $("#aiChatMessages").empty();
                }).fail(function () {
                    localStorage.removeItem("gophish_ai_conv");
                    $("#aiChatMessages").empty();
                });
            } else {
                $("#aiChatMessages").empty().append(chatBubble("assistant", "Bonjour ! Je suis l\'assistant gophish2. Dites-moi ce que vous voulez configurer : domaine de phishing, serveur SMTP, groupes de cibles, scénario de campagne..."));
            }
        });
        $("#aiChatSend").on("click", sendChatMessage);
        $("#aiChatInput").on("keydown", function (e) {
            if (e.which === 13) {
                e.preventDefault();
                sendChatMessage();
            }
        });
    });
})();
