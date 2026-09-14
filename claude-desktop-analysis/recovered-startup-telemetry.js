// Readable reconstruction of the relevant Claude Desktop 1.15962.1 paths.
// This is not Anthropic's original source and intentionally omits credentials.

const TELEMETRY_BATCH_SIZE = 50;
const TELEMETRY_FLUSH_DELAY_MS = 60_000;
const appSessionId = crypto.randomUUID();

async function buildDesktopEvent(eventName, eventProperties = {}) {
  const organizationId = deployment.telemetryOrgUuid() ?? await getOrgId();
  const appInfo = await getCachedAppInfo();
  const memory = process.getSystemMemoryInfo();
  const configSource = getConfigSource();

  const metadata = scrubTelemetry({
    product_surface: "claude-desktop",
    desktop_variant: "production",
    deployment_mode: deployment.type,
    config_source: configSource.type,
    config_source_remote: configSource.remote,
    ...classifyInferenceProvider(),
    app_version: appInfo.version,
    commit_hash: appInfo.commitHash,
    platform: process.platform,
    arch: process.arch,
    installer_variant: getInstallerVariant(),
    running_under_translation: electron.app.runningUnderARM64Translation,
    os_version: process.getSystemVersion(),
    os_release: os.release(),
    os_build: readSystemBuildVersion(),
    cpu_model: os.cpus()[0]?.model ?? null,
    total_memory: memory.total * 1024,
    free_memory: memory.free * 1024,
    ...getElectronProcessMetrics(),
    app_session_id: appSessionId,
    app_uptime_seconds: Math.floor(process.uptime()),
    window_count: electron.BrowserWindow.getAllWindows().length,
    ...(organizationId ? { organization_id: organizationId } : {}),
    ...eventProperties,
  });

  return {
    event_type: "TelemetryEvent",
    event_data: {
      event_name: eventName,
      timestamp: new Date().toISOString(),
      user_properties: { user_id: getUserId() || "anonymous" },
      metadata: JSON.stringify(metadata),
    },
  };
}

async function sendBatch(events) {
  const request = electron.net.request({
    method: "POST",
    url: `${getApiOrigin()}/api/event_logging/v2/batch`,
  });
  request.setHeader("Content-Type", "application/json");
  request.setHeader("x-service-name", "claude_desktop");
  request.write(JSON.stringify({ events }));
  request.end();
}

function logStartupPerformance(milestones, isFirstRun) {
  return logEvent("desktop_app_startup_perf", {
    main_imports_loaded_ms: milestones.mainImportsLoaded ?? null,
    app_ready_ms: milestones.appReady ?? null,
    window_created_ms: milestones.windowCreated ?? null,
    window_did_finish_load_ms: milestones.windowDidFinishLoad ?? null,
    main_view_dom_ready_ms: milestones.mainViewDomReady ?? null,
    is_first_run: isFirstRun,
  });
}

function installAnthropicRequestHeaders(session) {
  session.webRequest.onBeforeSendHeaders((details, done) => {
    if (!isAnthropicAllowedHost(new URL(details.url).host)) {
      done({ requestHeaders: details.requestHeaders });
      return;
    }

    done({
      requestHeaders: {
        ...details.requestHeaders,
        "anthropic-client-platform": "desktop_app",
        "anthropic-client-app": "com.anthropic.claudefordesktop",
        "anthropic-client-version": electron.app.getVersion(),
        "anthropic-client-os-platform": process.platform,
        "anthropic-client-os-version": process.getSystemVersion(),
        "anthropic-desktop-topbar": "1",
      },
    });
  });
}

function initializeSentry(settings) {
  if (settings.telemetry?.disableEssential || process.env.CI) return;

  Sentry.init({
    dsn: "https://<project>@o1158394.ingest.us.sentry.io/<project-id>",
    initialScope: {
      user: { id: getPersistentSentryUserId() },
      tags: { deployment_mode: isThirdPartyMode() ? "3p" : "1p" },
    },
    sendDefaultPii: false,
    beforeSend: scrubSentryEvent,
    beforeSendTransaction: scrubSentryEvent,
    beforeBreadcrumb: scrubBreadcrumb,
  });
}

function createAdditionalSentryContext(options = {}) {
  const config = {
    screen: true,
    deviceModelManufacturer: false,
    ...options,
  };
  const device = {};

  electron.app.whenReady().then(() => {
    if (!config.screen) return;
    const display = electron.screen.getPrimaryDisplay();
    device.screen_density = display.scaleFactor;
    device.screen_resolution =
      `${Math.floor(display.size.width * display.scaleFactor)}x` +
      `${Math.floor(display.size.height * display.scaleFactor)}`;
  });

  return async function addContextToSentryEvent(event) {
    // Default is false. This branch is dormant unless explicitly enabled.
    if (config.deviceModelManufacturer) {
      Object.assign(device, await readMacModel());
      config.deviceModelManufacturer = false;
    }
    return mergeContext(event, { device });
  };
}

async function readMacModel() {
  if (process.platform !== "darwin") return {};
  const stdout = await exec("system_profiler SPHardwareDataType -json");
  const model = JSON.parse(stdout)?.SPHardwareDataType?.[0]?.machine_model;
  return model ? { manufacturer: "Apple", model } : {};
}

function desktopInfoForSettingsPage() {
  return {
    app_version: electron.app.getVersion(),
    os_version: process.getSystemVersion(),
    cpu_model: os.cpus()[0]?.model ?? null,
    platform: process.platform,
    arch: process.arch,
    total_memory: os.totalmem(),
    can_elevate_to_admin: null,
    is_msix: isMsix(),
    installer_variant: getInstallerVariant(),
  };
}

async function growthBookExposure(experimentId, variationId) {
  return queueEvent({
    event_type: "GrowthbookExperimentEvent",
    event_data: {
      device_id: await readCookie("anthropic-device-id"),
      anonymous_id: await readCookie("ajs_anonymous_id"),
      timestamp: new Date().toISOString(),
      experiment_id: experimentId,
      variation_id: variationId,
      environment: "production",
    },
  });
}
