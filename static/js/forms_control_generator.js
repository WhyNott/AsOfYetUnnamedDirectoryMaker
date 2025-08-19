class FormControlGenerator {
    constructor(containerId, basicFieldsContainerId) {
        this.container = document.getElementById(containerId);
        this.basicFieldsContainer = document.getElementById(basicFieldsContainerId);
        this.basicFields = [];
        this.controls = new Map(); // Store control instances for value retrieval
        this.sliders = new Map(); // Store slider instances
    }

    generateControls(fieldPairs) {
        // Clear existing content
        this.container.innerHTML = '';
        this.basicFieldsContainer.innerHTML = '';
        this.basicFields = [];
        this.controls.clear();
        this.sliders.clear();

        fieldPairs.forEach(([label, type]) => {
            switch (type) {
                case 'basic':
                    this.handleBasicField(label);
                    break;
                case 'tag':
                    this.generateTagControl(label);
                    break;
                case 'category':
                    this.generateCategoryControl(label);
                    break;
                case 'location':
                    this.generateLocationControl(label);
                    break;
                case 'numeric':
                    this.generateNumericControl(label);
                    break;
                default:
                    console.warn(`Unknown control type: ${type}`);
            }
        });

        this.displayBasicFields();
    }

    handleBasicField(label) {
        this.basicFields.push(label);
    }

    generateTagControl(label) {
        const controlGroup = this.createControlGroup(label);
        const selectElement = document.createElement('select');
        selectElement.multiple = true;
        selectElement.id = `tag-${label.replace(/\s+/g, '-').toLowerCase()}`;

        controlGroup.appendChild(selectElement);
        this.container.appendChild(controlGroup);

        // Initialize Selectize.js for tags
        const selectizeInstance = $(selectElement).selectize({
            plugins: ['remove_button'],
            delimiter: ',',
            persist: false,
            create: function(input) {
                return {
                    value: input,
                    text: input
                }
            },
            placeholder: 'Type to add tags...'
        })[0].selectize;

        this.controls.set(label, {
            type: 'tag',
            instance: selectizeInstance,
            getValue: () => selectizeInstance.getValue()
        });
    }

    generateCategoryControl(label) {
        const controlGroup = this.createControlGroup(label);
        const selectElement = document.createElement('select');
        selectElement.multiple = true;
        selectElement.id = `category-${label.replace(/\s+/g, '-').toLowerCase()}`;

        controlGroup.appendChild(selectElement);
        this.container.appendChild(controlGroup);

        // Initialize Selectize.js for categories
        // This is kept separate from tags for future customization
        const selectizeInstance = $(selectElement).selectize({
            plugins: ['remove_button'],
            delimiter: ',',
            persist: false,
            create: function(input) {
                return {
                    value: input,
                    text: input
                }
            },
            placeholder: 'Select or create categories...'
        })[0].selectize;

        this.controls.set(label, {
            type: 'category',
            instance: selectizeInstance,
            getValue: () => selectizeInstance.getValue()
        });
    }

    generateLocationControl(label) {
        const controlGroup = this.createControlGroup(label);
        const selectElement = document.createElement('select');
        selectElement.multiple = true;
        selectElement.id = `location-${label.replace(/\s+/g, '-').toLowerCase()}`;

        controlGroup.appendChild(selectElement);
        this.container.appendChild(controlGroup);

        // Initialize Selectize.js for locations
        // This is kept separate from tags/categories for future customization
        const selectizeInstance = $(selectElement).selectize({
            plugins: ['remove_button'],
            delimiter: ',',
            persist: false,
            create: function(input) {
                return {
                    value: input,
                    text: input
                }
            },
            placeholder: 'Select or add locations...'
        })[0].selectize;

        this.controls.set(label, {
            type: 'location',
            instance: selectizeInstance,
            getValue: () => selectizeInstance.getValue()
        });
    }

    generateNumericControl(label) {
        const controlGroup = this.createControlGroup(label);

        // Create slider container
        const sliderContainer = document.createElement('div');
        sliderContainer.className = 'range-slider-container';

        const sliderElement = document.createElement('div');
        sliderElement.className = 'range-slider';
        sliderElement.id = `slider-${label.replace(/\s+/g, '-').toLowerCase()}`;

        // Create input fields for manual entry
        const inputGroup = document.createElement('div');
        inputGroup.className = 'range-input-group';

        const minInput = document.createElement('input');
        minInput.type = 'number';
        minInput.className = 'range-input';
        minInput.placeholder = 'Min value';

        const maxInput = document.createElement('input');
        maxInput.type = 'number';
        maxInput.className = 'range-input';
        maxInput.placeholder = 'Max value';

        const clearMinBtn = document.createElement('button');
        clearMinBtn.className = 'clear-btn';
        clearMinBtn.textContent = 'Clear Min';

        const clearMaxBtn = document.createElement('button');
        clearMaxBtn.className = 'clear-btn';
        clearMaxBtn.textContent = 'Clear Max';

        const valuesDisplay = document.createElement('div');
        valuesDisplay.className = 'range-values';
        valuesDisplay.innerHTML = '<span>Min: Not set</span><span>Max: Not set</span>';

        inputGroup.appendChild(minInput);
        inputGroup.appendChild(maxInput);
        inputGroup.appendChild(clearMinBtn);
        inputGroup.appendChild(clearMaxBtn);

        sliderContainer.appendChild(sliderElement);
        sliderContainer.appendChild(valuesDisplay);
        sliderContainer.appendChild(inputGroup);

        controlGroup.appendChild(sliderContainer);
        this.container.appendChild(controlGroup);

        // Initialize noUiSlider
        const slider = noUiSlider.create(sliderElement, {
            start: [20, 80],
            connect: true,
            range: {
                'min': 0,
                'max': 100
            },
            format: {
                to: value => Math.round(value),
                from: value => Number(value)
            }
        });

        // Update display when slider changes
        slider.on('update', (values, handle) => {
            const [min, max] = values;
            valuesDisplay.innerHTML = `<span>Min: ${min}</span><span>Max: ${max}</span>`;
            minInput.value = min;
            maxInput.value = max;
        });

        // Update slider when inputs change
        minInput.addEventListener('input', () => {
            const min = parseFloat(minInput.value) || 0;
            const max = parseFloat(maxInput.value) || slider.get()[1];
            slider.set([min, max]);
        });

        maxInput.addEventListener('input', () => {
            const min = parseFloat(minInput.value) || slider.get()[0];
            const max = parseFloat(maxInput.value) || 100;
            slider.set([min, max]);
        });

        // Clear button functionality
        clearMinBtn.addEventListener('click', () => {
            minInput.value = '';
            slider.set([null, slider.get()[1]]);
            valuesDisplay.innerHTML = `<span>Min: Not set</span><span>Max: ${slider.get()[1]}</span>`;
        });

        clearMaxBtn.addEventListener('click', () => {
            maxInput.value = '';
            slider.set([slider.get()[0], null]);
            valuesDisplay.innerHTML = `<span>Min: ${slider.get()[0]}</span><span>Max: Not set</span>`;
        });

        this.sliders.set(label, slider);
        this.controls.set(label, {
            type: 'numeric',
            instance: slider,
            minInput: minInput,
            maxInput: maxInput,
            getValue: () => {
                const values = slider.get();
                return {
                    min: minInput.value ? parseFloat(minInput.value) : null,
                    max: maxInput.value ? parseFloat(maxInput.value) : null,
                    sliderValues: values
                };
            }
        });
    }

    createControlGroup(label) {
        const controlGroup = document.createElement('div');
        controlGroup.className = 'control-group';

        const labelElement = document.createElement('label');
        labelElement.className = 'control-label';
        labelElement.textContent = label;

        controlGroup.appendChild(labelElement);
        return controlGroup;
    }

    displayBasicFields() {
        // Don't display basic fields heading - they will be searched via the search box
        return;
    }

    getAllValues() {
        const values = {
            basicFields: this.basicFields,
            controls: {}
        };

        this.controls.forEach((control, label) => {
            values.controls[label] = {
                type: control.type,
                value: control.getValue()
            };
        });

        return values;
    }

    clear() {
        this.container.innerHTML = '';
        this.basicFieldsContainer.innerHTML = '';
        this.basicFields = [];
        this.controls.clear();
        this.sliders.clear();
    }
}