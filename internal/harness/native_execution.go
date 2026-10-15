package harness

import (
	"fmt"
	"formal-gates/internal/engine/authoring"
	"os"
	"os/exec"
	"path/filepath"
)

// nativeExecutionBinding uses a captured binding with entries as issued.
func nativeExecutionBinding(request any, capture func() (map[string]any, error)) (map[string]any, error) {
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		return capture()
	}
	return binding, nil
}

func nativeBindingEnvironment(binding any) map[string]string {
	environment := map[string]string{}
	for key, value := range objectOrEmpty(getOf(binding, "environment")) {
		environment[key] = textOf(value)
	}
	return environment
}

// StopNativeJob uses the original supervisor observation/cancellation path.
func StopNativeJob(directory string) error {
	request, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		return err
	}
	digest, err := CanonicalDigest(request)
	if err != nil {
		return err
	}
	if _, err := observeSupervisor(directory, digest, true); err != nil {
		return fmt.Errorf("native cancellation: %w", err)
	}
	if request, err := readJSONFile(filepath.Join(directory, "native-request.json")); err == nil {
		if impl, err := nativeImplementationFor(textOf(getOf(request, "provider"))); err == nil && impl.cleanup != nil {
			return impl.cleanup(directory)
		}
	}
	return nil
}

// NativeJobSource observes the original completion, including a lost response.
// Its provider verifier remains the source of truth for the native evidence.
func NativeJobSource(provider, directory string, services ...ExecutionServices) (map[string]any, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, err
	}
	source, err := readJSONFile(filepath.Join(directory, "source.json"))
	if err != nil && os.IsNotExist(err) {
		process, processErr := readJSONFile(filepath.Join(directory, "exit.json"))
		if processErr != nil {
			return nil, processErr
		}
		source, err = impl.reconcile(directory, process, services...)
	}
	if err != nil {
		return nil, err
	}
	if err := impl.verifySource(directory, source); err != nil {
		return nil, err
	}
	if len(services) > 0 {
		request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			return nil, err
		}
		if err := settleQueuedMessages(directory, request, nil, services); err != nil {
			return nil, err
		}
	}
	return source.(map[string]any), nil
}

func NativeJobAlive(directory string) bool { return supervisorAlive(directory) }

// NativeStartEvidence consumes the adapter's actual create/resume/init
// acknowledgement, saved with identity before the model turn is issued.
func NativeStartEvidence(provider, directory string) (string, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return "", err
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return "", err
	}
	if getOf(request, "provider") != provider {
		return "", fmt.Errorf("native start provider differs from original request")
	}
	if err := impl.validateBinding(getOf(request, "binding")); err != nil {
		return "", err
	}
	start, err := readJSONFile(filepath.Join(directory, "identity.json"))
	if err != nil {
		return "", err
	}
	identity, _ := getOf(start, "identity").(string)
	if identity == "" || resolveNativePath(textOf(getOf(start, "workspace"))) != resolveNativePath(textOf(getOf(request, "workspace"))) {
		return "", fmt.Errorf("native start identity/workspace proof is missing or different")
	}
	if previous, _ := getOf(request, "identity").(string); previous != "" && previous != identity {
		return "", fmt.Errorf("native start resumed another identity")
	}
	if err := impl.verifyStart(request, getOf(start, "acknowledgement"), identity); err != nil {
		return "", err
	}
	return identity, nil
}

// NativeFailureEvidence observes the original failed invocation and supervisor
// receipt. It does not require or manufacture a successful model answer.
func NativeFailureEvidence(provider, directory string) ([]byte, string, string, error) {
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, "", "", err
	}
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, "", "", err
	}
	if getOf(request, "provider") != provider {
		return nil, "", "", fmt.Errorf("native failure provider differs")
	}
	if err := impl.validateBinding(getOf(request, "binding")); err != nil {
		return nil, "", "", err
	}
	outer, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		return nil, "", "", err
	}
	if !valuesEqual(getOf(outer, "engine"), getOf(request, "engine")) {
		return nil, "", "", fmt.Errorf("native failure request differs from original supervisor")
	}
	receipt, err := readJSONFile(filepath.Join(directory, "exit.json"))
	if err != nil {
		return nil, "", "", err
	}
	process, err := readJSONFile(filepath.Join(directory, "process.json"))
	if err != nil {
		return nil, "", "", err
	}
	if getOf(process, "request_digest") != getOf(receipt, "request_digest") {
		return nil, "", "", fmt.Errorf("native failure process differs from original request")
	}
	streams := objectOrEmpty(getOf(receipt, "streams"))
	proofs := []Proof{}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		proofs = append(proofs, Proof{Name: name, Expected: objectOrEmpty(streams[name])})
	}
	if _, refusal := VerifyProcess(directory, objectOrEmpty(receipt), objectOrEmpty(receipt), objectOrEmpty(outer), proofs); refusal != "" {
		return nil, "", "", fmt.Errorf("%s", refusal)
	}
	if intOf(getOf(receipt, "exit_code")) == 0 {
		return nil, "", "", fmt.Errorf("native invocation did not terminate as a failure")
	}
	path := filepath.Join(directory, "native-error.json")
	failure, err := readJSONFile(path)
	if err != nil {
		return nil, "", "", err
	}
	class := authoring.FailureClass(textOf(getOf(failure, "failure_class")))
	if getOf(failure, "invoked") != true || !class.Valid() {
		return nil, "", "", fmt.Errorf("native invocation failure was not observed")
	}
	identity := ""
	if _, err := os.Stat(filepath.Join(directory, "identity.json")); err == nil {
		identity, err = NativeStartEvidence(provider, directory)
		if err != nil {
			return nil, "", "", err
		}
	} else if !os.IsNotExist(err) {
		return nil, "", "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", err
	}
	proof, refusal := FileProof(path)
	if refusal != "" {
		return nil, "", "", fmt.Errorf("%s", refusal)
	}
	digest, err := CanonicalDigest(map[string]any{"request_digest": getOf(receipt, "request_digest"), "failure_digest": proof.SHA256})
	if err != nil {
		return nil, "", "", err
	}
	return body, "sha256:" + digest, identity, nil
}

// DetachSupervisor keeps an admitted native job independent of its foreground
// consumer. Cancellation still follows the original supervisor protocol.
func DetachSupervisor(cmd *exec.Cmd) { cmd.SysProcAttr = supervisorSysProcAttr() }

// prepareNativeInvocation claims the original job while engine admission still
// holds its native window. Recovery reads these same native evidence files.
func prepareNativeInvocation(directory string, request map[string]any, impl nativeImplementation) error {
	supervised := false
	if _, err := os.Stat(filepath.Join(directory, "owner")); err == nil {
		process, err := readJSONFile(filepath.Join(directory, "process.json"))
		if err != nil {
			return fmt.Errorf("native invocation is UNKNOWN without process evidence: %w", err)
		}
		// The original supervisor starts exactly this native command as its child.
		outer, err := readJSONFile(filepath.Join(directory, "request.json"))
		argv := extractArgs(getOf(outer, "argv"))
		if err == nil && intOf(getOf(process, "pid")) == os.Getppid() && supervisorAlive(directory) && len(argv) == 4 && argv[1] == "harness" && argv[2] == "native" && resolveNativePath(argv[3]) == resolveNativePath(directory) {
			supervised = true
		} else {
			return &blockedError{message: "native invocation already has an owner; observe its original result or UNKNOWN effect"}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := inheritNativeIdentity(directory, request, impl); err != nil {
		return err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		return err
	}
	if supervised {
		return nil
	}
	return claimJob(directory, request)
}

// inheritNativeIdentity only searches this original action's invocation facts.
// Every completed call must prove the same native identity; live or unknown
// calls cannot be replaced by another model request.
func inheritNativeIdentity(directory string, request map[string]any, impl nativeImplementation) error {
	entries, err := os.ReadDir(filepath.Dir(directory))
	if err != nil {
		return err
	}
	engine := getOf(request, "engine")
	material := getOf(engine, "workerMaterial")
	identity := ""
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == filepath.Base(directory) {
			continue
		}
		prior := filepath.Join(filepath.Dir(directory), entry.Name())
		if _, err := os.Stat(filepath.Join(prior, "owner")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		original, err := readJSONFile(filepath.Join(prior, "native-request.json"))
		if err != nil {
			return err
		}
		oldMaterial := getOf(getOf(original, "engine"), "workerMaterial")
		if !valuesEqual(getOf(oldMaterial, "actionId"), getOf(material, "actionId")) || !valuesEqual(getOf(oldMaterial, "attemptId"), getOf(material, "attemptId")) {
			return &blockedError{message: "native invocation evidence belongs to a different action/attempt"}
		}
		source, err := readJSONFile(filepath.Join(prior, "source.json"))
		observed := ""
		if err != nil {
			if supervisorAlive(prior) {
				return &blockedError{message: "original native invocation is still executing"}
			}
			_, _, observed, err = NativeFailureEvidence(textOf(getOf(request, "provider")), prior)
			if err != nil {
				return &blockedError{message: "original native invocation is UNKNOWN; reconcile before resume"}
			}
		} else {
			if err := impl.verifySource(prior, source); err != nil {
				return err
			}
			observed = textOf(getOf(source, "identity"))
		}
		if observed == "" || identity != "" && observed != identity {
			return &blockedError{message: "native invocation identities conflict"}
		}
		identity = observed
		if impl.inheritInvocation != nil {
			if err := impl.inheritInvocation(prior, request, source); err != nil {
				return err
			}
		}
	}
	requested, _ := request["identity"].(string)
	if identity == "" && requested != "" {
		return &blockedError{message: "native resume lacks original source evidence"}
	}
	if identity != "" {
		if requested != "" && requested != identity {
			return &blockedError{message: "native resume differs from original source identity"}
		}
		request["identity"] = identity
	}
	return nil
}
